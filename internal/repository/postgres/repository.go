package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) SaveBatch(ctx context.Context, records []domain.Record) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	for _, record := range records {
		_, err = tx.Exec(ctx, `
			INSERT INTO land_records (pnu, dataset, observed_at, payload)
			VALUES ($1, $2, $3, $4::jsonb)
			ON CONFLICT (pnu, dataset)
			DO UPDATE SET observed_at = EXCLUDED.observed_at, payload = EXCLUDED.payload
		`, record.PNU, string(record.Dataset), record.ObservedAt, record.PayloadJSON)
		if err != nil {
			return fmt.Errorf("save %s/%s: %w", record.PNU, record.Dataset, err)
		}

		_, err = tx.Exec(ctx, `
			DELETE FROM failed_requests
			WHERE pnu = $1 AND dataset = $2
		`, record.PNU, string(record.Dataset))
		if err != nil {
			return fmt.Errorf("clear failure %s/%s: %w", record.PNU, record.Dataset, err)
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) SaveFailure(ctx context.Context, failure domain.Failure) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO failed_requests (pnu, dataset, failed_at, error_text, attempts)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (pnu, dataset)
		DO UPDATE SET
			failed_at = EXCLUDED.failed_at,
			error_text = EXCLUDED.error_text,
			attempts = failed_requests.attempts + 1
	`, failure.PNU, string(failure.Dataset), failure.FailedAt, failure.ErrorText)
	if err != nil {
		return fmt.Errorf("save failure %s/%s: %w", failure.PNU, failure.Dataset, err)
	}
	return nil
}

func (r *Repository) ListRegions(ctx context.Context) ([]domain.Region, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT code, name, level, COALESCE(parent_code, '')
		FROM regions
		ORDER BY CASE level WHEN 'sido' THEN 0 ELSE 1 END, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var regions []domain.Region
	for rows.Next() {
		var region domain.Region
		if err := rows.Scan(&region.Code, &region.Name, &region.Level, &region.ParentCode); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}
	return regions, rows.Err()
}

func (r *Repository) ListSchedules(ctx context.Context) ([]domain.Schedule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.name, s.region_code, r.name, s.frequency, s.hour, s.minute,
		       COALESCE(s.weekday, -1), COALESCE(s.day_of_month, 0), s.datasets,
		       s.enabled, s.last_run_at, s.created_at, s.updated_at
		FROM collection_schedules s
		JOIN regions r ON r.code = s.region_code
		ORDER BY s.enabled DESC, s.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []domain.Schedule
	for rows.Next() {
		var schedule domain.Schedule
		if err := rows.Scan(
			&schedule.ID,
			&schedule.Name,
			&schedule.RegionCode,
			&schedule.RegionName,
			&schedule.Frequency,
			&schedule.Hour,
			&schedule.Minute,
			&schedule.Weekday,
			&schedule.DayOfMonth,
			&schedule.Datasets,
			&schedule.Enabled,
			&schedule.LastRunAt,
			&schedule.CreatedAt,
			&schedule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}

func (r *Repository) CreateSchedule(ctx context.Context, schedule domain.Schedule) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO collection_schedules
			(name, region_code, frequency, hour, minute, weekday, day_of_month, datasets, enabled)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, -1), NULLIF($7, 0), $8, true)
	`,
		schedule.Name,
		schedule.RegionCode,
		schedule.Frequency,
		schedule.Hour,
		schedule.Minute,
		schedule.Weekday,
		schedule.DayOfMonth,
		schedule.Datasets,
	)
	return err
}

func (r *Repository) ToggleSchedule(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE collection_schedules
		SET enabled = NOT enabled, updated_at = now()
		WHERE id = $1
	`, id)
	return err
}

func (r *Repository) DeleteSchedule(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM collection_schedules WHERE id = $1`, id)
	return err
}

func (r *Repository) MarkScheduleRun(ctx context.Context, id int64, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE collection_schedules
		SET last_run_at = $2, updated_at = now()
		WHERE id = $1
	`, id, at)
	return err
}

func (r *Repository) CreateRun(ctx context.Context, run domain.CollectionRun) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO collection_runs
			(schedule_id, region_code, region_name, trigger_type, started_at, status, message)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`,
		run.ScheduleID,
		run.RegionCode,
		run.RegionName,
		run.TriggerType,
		run.StartedAt,
		run.Status,
		run.Message,
	).Scan(&id)
	return id, err
}

func (r *Repository) FinishRun(ctx context.Context, id int64, status, message string, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE collection_runs
		SET finished_at = $2, status = $3, message = $4
		WHERE id = $1
	`, id, at, status, message)
	return err
}

func (r *Repository) ListRuns(ctx context.Context, limit int) ([]domain.CollectionRun, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, schedule_id, region_code, region_name, trigger_type,
		       started_at, finished_at, status, message
		FROM collection_runs
		ORDER BY started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []domain.CollectionRun
	for rows.Next() {
		var run domain.CollectionRun
		if err := rows.Scan(
			&run.ID,
			&run.ScheduleID,
			&run.RegionCode,
			&run.RegionName,
			&run.TriggerType,
			&run.StartedAt,
			&run.FinishedAt,
			&run.Status,
			&run.Message,
		); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *Repository) ListFailures(ctx context.Context, limit int) ([]domain.Failure, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT pnu, dataset, failed_at, error_text
		FROM failed_requests
		ORDER BY failed_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var failures []domain.Failure
	for rows.Next() {
		var failure domain.Failure
		if err := rows.Scan(&failure.PNU, &failure.Dataset, &failure.FailedAt, &failure.ErrorText); err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}

func (r *Repository) DashboardStats(ctx context.Context) (domain.DashboardStats, error) {
	var stats domain.DashboardStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM collection_schedules WHERE enabled),
			(SELECT count(*) FROM collection_runs WHERE status = 'RUNNING'),
			(SELECT count(*) FROM collection_runs WHERE status = 'SUCCESS' AND started_at >= CURRENT_DATE),
			(SELECT count(*) FROM collection_runs WHERE status = 'FAILED' AND started_at >= CURRENT_DATE),
			(SELECT count(*) FROM failed_requests)
	`).Scan(
		&stats.ActiveSchedules,
		&stats.RunningRuns,
		&stats.TodaySuccess,
		&stats.TodayFailed,
		&stats.FailedRequests,
	)
	return stats, err
}
