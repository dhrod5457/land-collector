package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
	_ "modernc.org/sqlite"
)

type Repository struct {
	db *sql.DB
}

func New(ctx context.Context, path string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}

	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Close() { _ = r.db.Close() }

func migrate(ctx context.Context, db *sql.DB) error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS land_records (
			pnu TEXT NOT NULL,
			dataset TEXT NOT NULL,
			observed_at INTEGER NOT NULL,
			payload TEXT NOT NULL,
			PRIMARY KEY (pnu, dataset)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_land_records_dataset_observed_at
			ON land_records(dataset, observed_at DESC)`,
		`CREATE TABLE IF NOT EXISTS failed_requests (
			pnu TEXT NOT NULL,
			dataset TEXT NOT NULL,
			failed_at INTEGER NOT NULL,
			error_text TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (pnu, dataset)
		)`,
		`CREATE TABLE IF NOT EXISTS regions (
			code TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			level TEXT NOT NULL,
			parent_code TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS collection_schedules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			region_code TEXT NOT NULL REFERENCES regions(code),
			frequency TEXT NOT NULL CHECK (frequency IN ('daily','weekly','monthly')),
			hour INTEGER NOT NULL CHECK (hour BETWEEN 0 AND 23),
			minute INTEGER NOT NULL CHECK (minute BETWEEN 0 AND 59),
			weekday INTEGER,
			day_of_month INTEGER,
			datasets TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_run_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS collection_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			schedule_id INTEGER REFERENCES collection_schedules(id) ON DELETE SET NULL,
			region_code TEXT NOT NULL,
			region_name TEXT NOT NULL,
			trigger_type TEXT NOT NULL,
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			status TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_runs_started_at
			ON collection_runs(started_at DESC)`,
	}

	for _, stmt := range schema {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	regions := []domain.Region{
		{Code:"11", Name:"서울특별시", Level:"sido"},
		{Code:"26", Name:"부산광역시", Level:"sido"},
		{Code:"27", Name:"대구광역시", Level:"sido"},
		{Code:"28", Name:"인천광역시", Level:"sido"},
		{Code:"29", Name:"광주광역시", Level:"sido"},
		{Code:"30", Name:"대전광역시", Level:"sido"},
		{Code:"31", Name:"울산광역시", Level:"sido"},
		{Code:"36", Name:"세종특별자치시", Level:"sido"},
		{Code:"41", Name:"경기도", Level:"sido"},
		{Code:"43", Name:"충청북도", Level:"sido"},
		{Code:"44", Name:"충청남도", Level:"sido"},
		{Code:"46", Name:"전라남도", Level:"sido"},
		{Code:"47", Name:"경상북도", Level:"sido"},
		{Code:"48", Name:"경상남도", Level:"sido"},
		{Code:"50", Name:"제주특별자치도", Level:"sido"},
		{Code:"51", Name:"강원특별자치도", Level:"sido"},
		{Code:"52", Name:"전북특별자치도", Level:"sido"},
		{Code:"50110", Name:"제주시", Level:"sigungu", ParentCode:"50"},
		{Code:"50130", Name:"서귀포시", Level:"sigungu", ParentCode:"50"},
	}
	for _, region := range regions {
		if _, err := db.ExecContext(ctx,
			`INSERT OR IGNORE INTO regions(code,name,level,parent_code) VALUES(?,?,?,NULLIF(?,''))`,
			region.Code, region.Name, region.Level, region.ParentCode,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) SaveBatch(ctx context.Context, records []domain.Record) error {
	if len(records) == 0 { return nil }

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()

	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO land_records(pnu,dataset,observed_at,payload)
			VALUES(?,?,?,?)
			ON CONFLICT(pnu,dataset) DO UPDATE SET
				observed_at=excluded.observed_at,
				payload=excluded.payload
		`, record.PNU, string(record.Dataset), record.ObservedAt.Unix(), string(record.PayloadJSON)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM failed_requests WHERE pnu=? AND dataset=?`,
			record.PNU, string(record.Dataset),
		); err != nil { return err }
	}
	return tx.Commit()
}

func (r *Repository) SaveFailure(ctx context.Context, failure domain.Failure) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO failed_requests(pnu,dataset,failed_at,error_text,attempts)
		VALUES(?,?,?,?,1)
		ON CONFLICT(pnu,dataset) DO UPDATE SET
			failed_at=excluded.failed_at,
			error_text=excluded.error_text,
			attempts=failed_requests.attempts+1
	`, failure.PNU, string(failure.Dataset), failure.FailedAt.Unix(), failure.ErrorText)
	return err
}

func (r *Repository) ListRegions(ctx context.Context) ([]domain.Region, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT code,name,level,COALESCE(parent_code,'')
		FROM regions ORDER BY CASE level WHEN 'sido' THEN 0 ELSE 1 END, code
	`)
	if err != nil { return nil, err }
	defer rows.Close()

	var result []domain.Region
	for rows.Next() {
		var v domain.Region
		if err := rows.Scan(&v.Code,&v.Name,&v.Level,&v.ParentCode); err != nil { return nil, err }
		result = append(result,v)
	}
	return result, rows.Err()
}

func (r *Repository) ListSchedules(ctx context.Context) ([]domain.Schedule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id,s.name,s.region_code,r.name,s.frequency,s.hour,s.minute,
		       COALESCE(s.weekday,-1),COALESCE(s.day_of_month,0),s.datasets,
		       s.enabled,s.last_run_at,s.created_at,s.updated_at
		FROM collection_schedules s JOIN regions r ON r.code=s.region_code
		ORDER BY s.enabled DESC,s.id DESC
	`)
	if err != nil { return nil, err }
	defer rows.Close()

	var result []domain.Schedule
	for rows.Next() {
		var v domain.Schedule
		var enabled int
		var last sql.NullInt64
		var created, updated int64
		if err := rows.Scan(&v.ID,&v.Name,&v.RegionCode,&v.RegionName,&v.Frequency,&v.Hour,&v.Minute,
			&v.Weekday,&v.DayOfMonth,&v.Datasets,&enabled,&last,&created,&updated); err != nil { return nil, err }
		v.Enabled = enabled != 0
		if last.Valid { t:=time.Unix(last.Int64,0).UTC(); v.LastRunAt=&t }
		v.CreatedAt=time.Unix(created,0).UTC()
		v.UpdatedAt=time.Unix(updated,0).UTC()
		result=append(result,v)
	}
	return result,rows.Err()
}

func (r *Repository) CreateSchedule(ctx context.Context, s domain.Schedule) error {
	now:=time.Now().UTC().Unix()
	_,err:=r.db.ExecContext(ctx,`
		INSERT INTO collection_schedules
			(name,region_code,frequency,hour,minute,weekday,day_of_month,datasets,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,NULLIF(?,-1),NULLIF(?,0),?,1,?,?)
	`,s.Name,s.RegionCode,s.Frequency,s.Hour,s.Minute,s.Weekday,s.DayOfMonth,s.Datasets,now,now)
	return err
}

func (r *Repository) ToggleSchedule(ctx context.Context,id int64) error {
	_,err:=r.db.ExecContext(ctx,`
		UPDATE collection_schedules
		SET enabled=CASE enabled WHEN 1 THEN 0 ELSE 1 END, updated_at=?
		WHERE id=?
	`,time.Now().UTC().Unix(),id)
	return err
}

func (r *Repository) DeleteSchedule(ctx context.Context,id int64) error {
	_,err:=r.db.ExecContext(ctx,`DELETE FROM collection_schedules WHERE id=?`,id)
	return err
}

func (r *Repository) MarkScheduleRun(ctx context.Context,id int64,at time.Time) error {
	_,err:=r.db.ExecContext(ctx,`
		UPDATE collection_schedules SET last_run_at=?,updated_at=? WHERE id=?
	`,at.Unix(),time.Now().UTC().Unix(),id)
	return err
}

func (r *Repository) CreateRun(ctx context.Context,run domain.CollectionRun)(int64,error){
	result,err:=r.db.ExecContext(ctx,`
		INSERT INTO collection_runs(schedule_id,region_code,region_name,trigger_type,started_at,status,message)
		VALUES(?,?,?,?,?,?,?)
	`,run.ScheduleID,run.RegionCode,run.RegionName,run.TriggerType,run.StartedAt.Unix(),run.Status,run.Message)
	if err!=nil{return 0,err}
	return result.LastInsertId()
}

func (r *Repository) FinishRun(ctx context.Context,id int64,status,message string,at time.Time) error {
	_,err:=r.db.ExecContext(ctx,`
		UPDATE collection_runs SET finished_at=?,status=?,message=? WHERE id=?
	`,at.Unix(),status,message,id)
	return err
}

func (r *Repository) ListRuns(ctx context.Context,limit int)([]domain.CollectionRun,error){
	rows,err:=r.db.QueryContext(ctx,`
		SELECT id,schedule_id,region_code,region_name,trigger_type,started_at,finished_at,status,message
		FROM collection_runs ORDER BY started_at DESC LIMIT ?
	`,limit)
	if err!=nil{return nil,err}
	defer rows.Close()

	var result []domain.CollectionRun
	for rows.Next(){
		var v domain.CollectionRun
		var scheduleID,finished sql.NullInt64
		var started int64
		if err:=rows.Scan(&v.ID,&scheduleID,&v.RegionCode,&v.RegionName,&v.TriggerType,&started,&finished,&v.Status,&v.Message);err!=nil{return nil,err}
		if scheduleID.Valid{x:=scheduleID.Int64;v.ScheduleID=&x}
		v.StartedAt=time.Unix(started,0).UTC()
		if finished.Valid{t:=time.Unix(finished.Int64,0).UTC();v.FinishedAt=&t}
		result=append(result,v)
	}
	return result,rows.Err()
}

func (r *Repository) ListFailures(ctx context.Context,limit int)([]domain.Failure,error){
	rows,err:=r.db.QueryContext(ctx,`
		SELECT pnu,dataset,failed_at,error_text FROM failed_requests
		ORDER BY failed_at DESC LIMIT ?
	`,limit)
	if err!=nil{return nil,err}
	defer rows.Close()

	var result []domain.Failure
	for rows.Next(){
		var v domain.Failure
		var failed int64
		if err:=rows.Scan(&v.PNU,&v.Dataset,&failed,&v.ErrorText);err!=nil{return nil,err}
		v.FailedAt=time.Unix(failed,0).UTC()
		result=append(result,v)
	}
	return result,rows.Err()
}

func (r *Repository) DashboardStats(ctx context.Context)(domain.DashboardStats,error){
	var v domain.DashboardStats
	startOfDay:=time.Now().UTC().Truncate(24*time.Hour).Unix()
	err:=r.db.QueryRowContext(ctx,`
		SELECT
			(SELECT count(*) FROM collection_schedules WHERE enabled=1),
			(SELECT count(*) FROM collection_runs WHERE status='RUNNING'),
			(SELECT count(*) FROM collection_runs WHERE status='SUCCESS' AND started_at>=?),
			(SELECT count(*) FROM collection_runs WHERE status='FAILED' AND started_at>=?),
			(SELECT count(*) FROM failed_requests)
	`,startOfDay,startOfDay).Scan(&v.ActiveSchedules,&v.RunningRuns,&v.TodaySuccess,&v.TodayFailed,&v.FailedRequests)
	return v,err
}
