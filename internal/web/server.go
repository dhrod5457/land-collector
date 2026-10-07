package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/scheduler"
)

//go:embed templates/*.html
var templateFS embed.FS

type Repository interface {
	ListRegions(context.Context) ([]domain.Region, error)
	ListSchedules(context.Context) ([]domain.Schedule, error)
	CreateSchedule(context.Context, domain.Schedule) error
	ToggleSchedule(context.Context, int64) error
	DeleteSchedule(context.Context, int64) error
	ListRuns(context.Context, int) ([]domain.CollectionRun, error)
	ListFailures(context.Context, int) ([]domain.Failure, error)
	DashboardStats(context.Context) (domain.DashboardStats, error)
}

type Runner interface {
	RunManual(context.Context, string, string, string) error
}

type Server struct {
	repository Repository
	runner     Runner
	username   string
	password   string
	templates  *template.Template
}

type pageData struct {
	Page      string
	Stats     domain.DashboardStats
	Regions   []domain.Region
	Schedules []domain.Schedule
	Runs      []domain.CollectionRun
	Failures  []domain.Failure
	Message   string
	Error     string
}

func NewServer(repository Repository, runner Runner, username, password string) (*Server, error) {
	kst := time.FixedZone("KST", 9*60*60)
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.In(kst).Format("2006-01-02 15:04:05")
		},
		"formatTimePtr": func(t *time.Time) string {
			if t == nil {
				return "-"
			}
			return t.In(kst).Format("2006-01-02 15:04:05")
		},
		"scheduleText": scheduleText,
		"statusClass":  statusClass,
		"datasetText":  datasetText,
	}
	tmpl, err := template.New("admin.html").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		repository: repository,
		runner:     runner,
		username:   username,
		password:   password,
		templates:  tmpl,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("POST /admin/run", s.handleRun)
	mux.HandleFunc("POST /admin/schedules", s.handleCreateSchedule)
	mux.HandleFunc("POST /admin/schedules/{id}/toggle", s.handleToggleSchedule)
	mux.HandleFunc("POST /admin/schedules/{id}/delete", s.handleDeleteSchedule)
	return s.basicAuth(mux)
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "dashboard"
	}

	data := pageData{
		Page:    page,
		Message: r.URL.Query().Get("message"),
		Error:   r.URL.Query().Get("error"),
	}

	var err error
	data.Stats, err = s.repository.DashboardStats(r.Context())
	if err != nil {
		s.renderError(w, err)
		return
	}
	data.Regions, err = s.repository.ListRegions(r.Context())
	if err != nil {
		s.renderError(w, err)
		return
	}
	data.Schedules, err = s.repository.ListSchedules(r.Context())
	if err != nil {
		s.renderError(w, err)
		return
	}
	data.Runs, err = s.repository.ListRuns(r.Context(), 100)
	if err != nil {
		s.renderError(w, err)
		return
	}
	data.Failures, err = s.repository.ListFailures(r.Context(), 100)
	if err != nil {
		s.renderError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "admin.html", data); err != nil {
		log.Printf("render admin: %v", err)
	}
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.redirect(w, r, "collect", "", "잘못된 요청입니다.")
		return
	}

	regionCode := r.FormValue("region_code")
	regionName, ok := s.regionName(r.Context(), regionCode)
	if !ok {
		s.redirect(w, r, "collect", "", "유효한 지역을 선택해 주세요.")
		return
	}

	datasets := selectedDatasets(r)
	if datasets == "" {
		s.redirect(w, r, "collect", "", "수집 대상을 하나 이상 선택해 주세요.")
		return
	}

	if err := s.runner.RunManual(r.Context(), regionCode, regionName, datasets); err != nil {
		s.redirect(w, r, "collect", "", err.Error())
		return
	}
	s.redirect(w, r, "runs", "수집 작업을 시작했습니다.", "")
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.redirect(w, r, "schedules", "", "잘못된 요청입니다.")
		return
	}

	regionCode := r.FormValue("region_code")
	regionName, ok := s.regionName(r.Context(), regionCode)
	if !ok {
		s.redirect(w, r, "schedules", "", "유효한 지역을 선택해 주세요.")
		return
	}

	hour, minute, err := scheduler.ParseClock(r.FormValue("time"))
	if err != nil {
		s.redirect(w, r, "schedules", "", "실행 시간을 확인해 주세요.")
		return
	}

	frequency := r.FormValue("frequency")
	if frequency != "daily" && frequency != "weekly" && frequency != "monthly" {
		s.redirect(w, r, "schedules", "", "지원하지 않는 실행 주기입니다.")
		return
	}

	weekday := -1
	dayOfMonth := 0
	if frequency == "weekly" {
		weekday, err = strconv.Atoi(r.FormValue("weekday"))
		if err != nil || weekday < 0 || weekday > 6 {
			s.redirect(w, r, "schedules", "", "요일을 확인해 주세요.")
			return
		}
	}
	if frequency == "monthly" {
		dayOfMonth, err = strconv.Atoi(r.FormValue("day_of_month"))
		if err != nil || dayOfMonth < 1 || dayOfMonth > 31 {
			s.redirect(w, r, "schedules", "", "실행일을 확인해 주세요.")
			return
		}
	}

	datasets := selectedDatasets(r)
	if datasets == "" {
		s.redirect(w, r, "schedules", "", "수집 대상을 하나 이상 선택해 주세요.")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = regionName + " 정기 수집"
	}

	err = s.repository.CreateSchedule(r.Context(), domain.Schedule{
		Name:       name,
		RegionCode: regionCode,
		RegionName: regionName,
		Frequency:  frequency,
		Hour:       hour,
		Minute:     minute,
		Weekday:    weekday,
		DayOfMonth: dayOfMonth,
		Datasets:   datasets,
	})
	if err != nil {
		s.redirect(w, r, "schedules", "", err.Error())
		return
	}
	s.redirect(w, r, "schedules", "스케줄을 등록했습니다.", "")
}

func (s *Server) handleToggleSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.redirect(w, r, "schedules", "", "잘못된 스케줄 ID입니다.")
		return
	}
	if err := s.repository.ToggleSchedule(r.Context(), id); err != nil {
		s.redirect(w, r, "schedules", "", err.Error())
		return
	}
	s.redirect(w, r, "schedules", "스케줄 상태를 변경했습니다.", "")
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.redirect(w, r, "schedules", "", "잘못된 스케줄 ID입니다.")
		return
	}
	if err := s.repository.DeleteSchedule(r.Context(), id); err != nil {
		s.redirect(w, r, "schedules", "", err.Error())
		return
	}
	s.redirect(w, r, "schedules", "스케줄을 삭제했습니다.", "")
}

func (s *Server) regionName(ctx context.Context, code string) (string, bool) {
	regions, err := s.repository.ListRegions(ctx)
	if err != nil {
		return "", false
	}
	for _, region := range regions {
		if region.Code == code {
			return region.Name, true
		}
	}
	return "", false
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, page, message, errorMessage string) {
	values := url.Values{"page": []string{page}}
	if message != "" {
		values.Set("message", message)
	}
	if errorMessage != "" {
		values.Set("error", errorMessage)
	}
	http.Redirect(w, r, "/admin?"+values.Encode(), http.StatusSeeOther)
}

func (s *Server) renderError(w http.ResponseWriter, err error) {
	log.Printf("admin error: %v", err)
	http.Error(w, "관리 화면 데이터를 불러오지 못했습니다.", http.StatusInternalServerError)
}

func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		username, password, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="land-collector admin"`)
			http.Error(w, "인증이 필요합니다.", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func selectedDatasets(r *http.Request) string {
	allowed := []string{"land", "characteristic", "price", "use_plan"}
	var selected []string
	for _, dataset := range allowed {
		if r.FormValue("dataset_"+dataset) == "on" {
			selected = append(selected, dataset)
		}
	}
	return strings.Join(selected, ",")
}

func scheduleText(s domain.Schedule) string {
	clock := fmt.Sprintf("%02d:%02d", s.Hour, s.Minute)
	switch s.Frequency {
	case "daily":
		return "매일 " + clock
	case "weekly":
		names := []string{"일", "월", "화", "수", "목", "금", "토"}
		if s.Weekday >= 0 && s.Weekday < len(names) {
			return "매주 " + names[s.Weekday] + "요일 " + clock
		}
	case "monthly":
		return fmt.Sprintf("매월 %d일 %s", s.DayOfMonth, clock)
	}
	return clock
}

func statusClass(status string) string {
	switch status {
	case "SUCCESS":
		return "bg-green-lt text-green"
	case "RUNNING":
		return "bg-blue-lt text-blue"
	case "PARTIAL_SUCCESS":
		return "bg-yellow-lt text-yellow"
	case "FAILED":
		return "bg-red-lt text-red"
	default:
		return "bg-secondary-lt"
	}
}

func datasetText(value string) string {
	r := strings.NewReplacer(
		"land", "토지·임야",
		"characteristic", "토지특성",
		"price", "개별공시지가",
		"use_plan", "토지이용계획",
		",", ", ",
	)
	return r.Replace(value)
}
