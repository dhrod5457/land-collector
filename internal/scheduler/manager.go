package scheduler

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhrod5457/land-collector/internal/collector"
	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/provider/nsdi"
)

type Repository interface {
	ListSchedules(context.Context) ([]domain.Schedule, error)
	MarkScheduleRun(context.Context, int64, time.Time) error
	CreateRun(context.Context, domain.CollectionRun) (int64, error)
	FinishRun(context.Context, int64, string, string, time.Time) error
}

type Manager struct {
	appCtx     context.Context
	repository Repository
	fetcher    collector.Fetcher
	saver      collector.Saver
	endpoints  map[domain.Dataset]nsdi.Endpoint
	workers    int
	batchSize  int
	pnuFile    string
	location   *time.Location

	mu     sync.Mutex
	active map[string]struct{}
}

func New(
	appCtx context.Context,
	repository Repository,
	fetcher collector.Fetcher,
	saver collector.Saver,
	endpoints []nsdi.Endpoint,
	workers int,
	batchSize int,
	pnuFile string,
	location *time.Location,
) *Manager {
	endpointMap := make(map[domain.Dataset]nsdi.Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		endpointMap[endpoint.Dataset] = endpoint
	}
	return &Manager{
		appCtx:     appCtx,
		repository: repository,
		fetcher:    fetcher,
		saver:      saver,
		endpoints:  endpointMap,
		workers:    workers,
		batchSize:  batchSize,
		pnuFile:    pnuFile,
		location:   location,
		active:     make(map[string]struct{}),
	}
}

func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	m.runDue(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.runDue(ctx, now)
		}
	}
}

func (m *Manager) RunManual(
	ctx context.Context,
	regionCode string,
	regionName string,
	datasets string,
) error {
	return m.startRun(m.appCtx, nil, regionCode, regionName, datasets, "manual")
}

func (m *Manager) runDue(ctx context.Context, now time.Time) {
	schedules, err := m.repository.ListSchedules(ctx)
	if err != nil {
		log.Printf("scheduler: list schedules: %v", err)
		return
	}

	for _, schedule := range schedules {
		if !schedule.Enabled || !IsDue(schedule, now.In(m.location), m.location) {
			continue
		}

		runAt := now.UTC()
		if err := m.repository.MarkScheduleRun(ctx, schedule.ID, runAt); err != nil {
			log.Printf("scheduler: mark schedule %d: %v", schedule.ID, err)
			continue
		}

		scheduleID := schedule.ID
		if err := m.startRun(
			ctx,
			&scheduleID,
			schedule.RegionCode,
			schedule.RegionName,
			schedule.Datasets,
			"scheduled",
		); err != nil {
			log.Printf("scheduler: start schedule %d: %v", schedule.ID, err)
		}
	}
}

func (m *Manager) startRun(
	ctx context.Context,
	scheduleID *int64,
	regionCode string,
	regionName string,
	datasets string,
	triggerType string,
) error {
	if regionCode == "" {
		return fmt.Errorf("region code is required")
	}

	m.mu.Lock()
	if _, exists := m.active[regionCode]; exists {
		m.mu.Unlock()
		return fmt.Errorf("region %s is already running", regionCode)
	}
	m.active[regionCode] = struct{}{}
	m.mu.Unlock()

	selected, err := m.selectEndpoints(datasets)
	if err != nil {
		m.release(regionCode)
		return err
	}

	runID, err := m.repository.CreateRun(ctx, domain.CollectionRun{
		ScheduleID:  scheduleID,
		RegionCode:  regionCode,
		RegionName:  regionName,
		TriggerType: triggerType,
		StartedAt:   time.Now().UTC(),
		Status:      "RUNNING",
	})
	if err != nil {
		m.release(regionCode)
		return err
	}

	go func() {
		defer m.release(regionCode)

		runner := collector.NewRunner(m.fetcher, m.saver, selected, m.workers, m.batchSize)
		runErr := runner.RunRegionFile(ctx, m.pnuFile, regionCode)

		status := "SUCCESS"
		message := ""
		if runErr != nil {
			status = "FAILED"
			message = runErr.Error()
			if strings.Contains(message, "completed with") {
				status = "PARTIAL_SUCCESS"
			}
		}
		if err := m.repository.FinishRun(context.WithoutCancel(ctx), runID, status, message, time.Now().UTC()); err != nil {
			log.Printf("scheduler: finish run %d: %v", runID, err)
		}
	}()

	return nil
}

func (m *Manager) selectEndpoints(csv string) ([]nsdi.Endpoint, error) {
	var selected []nsdi.Endpoint
	for _, value := range strings.Split(csv, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		dataset := domain.Dataset(value)
		endpoint, ok := m.endpoints[dataset]
		if !ok {
			return nil, fmt.Errorf("unsupported dataset %q", value)
		}
		selected = append(selected, endpoint)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one dataset is required")
	}
	return selected, nil
}

func (m *Manager) release(regionCode string) {
	m.mu.Lock()
	delete(m.active, regionCode)
	m.mu.Unlock()
}

func IsDue(schedule domain.Schedule, now time.Time, location *time.Location) bool {
	if !schedule.Enabled {
		return false
	}

	scheduled := time.Date(now.Year(), now.Month(), now.Day(), schedule.Hour, schedule.Minute, 0, 0, location)
	switch schedule.Frequency {
	case "daily":
	case "weekly":
		if int(now.Weekday()) != schedule.Weekday {
			return false
		}
	case "monthly":
		if now.Day() != schedule.DayOfMonth {
			return false
		}
	default:
		return false
	}

	if now.Before(scheduled) {
		return false
	}
	if schedule.LastRunAt == nil {
		return true
	}

	last := schedule.LastRunAt.In(location)
	switch schedule.Frequency {
	case "daily":
		return last.YearDay() != now.YearDay() || last.Year() != now.Year()
	case "weekly":
		y1, w1 := last.ISOWeek()
		y2, w2 := now.ISOWeek()
		return y1 != y2 || w1 != w2
	case "monthly":
		return last.Year() != now.Year() || last.Month() != now.Month()
	default:
		return false
	}
}

func ParseClock(value string) (int, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	return hour, minute, nil
}
