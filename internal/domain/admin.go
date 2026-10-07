package domain

import "time"

type Region struct {
	Code       string
	Name       string
	Level      string
	ParentCode string
}

type Schedule struct {
	ID         int64
	Name       string
	RegionCode string
	RegionName string
	Frequency  string
	Hour       int
	Minute     int
	Weekday    int
	DayOfMonth int
	Datasets   string
	Enabled    bool
	LastRunAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type CollectionRun struct {
	ID          int64
	ScheduleID  *int64
	RegionCode  string
	RegionName  string
	TriggerType string
	StartedAt   time.Time
	FinishedAt  *time.Time
	Status      string
	Message     string
}

type DashboardStats struct {
	ActiveSchedules int
	RunningRuns     int
	TodaySuccess    int
	TodayFailed     int
	FailedRequests  int
}
