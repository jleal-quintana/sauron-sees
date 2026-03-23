package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"sauron-sees/internal/filelock"
)

const (
	StatusOpen   = "open"
	StatusSealed = "sealed"
	StatusClosed = "closed"
	StatusFailed = "failed"
)

type FileState struct {
	LastSuccessfulClose string                `json:"last_successful_close"`
	PausedUntil         string                `json:"paused_until,omitempty"`
	Days                map[string]*DayState  `json:"days"`
	Weeks               map[string]*WeekState `json:"weeks"`
}

type DayState struct {
	Date                string         `json:"date"`
	Status              string         `json:"status"`
	LastCaptureAt       string         `json:"last_capture_at,omitempty"`
	CloseAttempts       int            `json:"close_attempts"`
	LastCloseAttemptAt  string         `json:"last_close_attempt_at,omitempty"`
	LastError           string         `json:"last_error,omitempty"`
	SummaryPath         string         `json:"summary_path,omitempty"`
	LastVerifierResult  string         `json:"last_verifier_result,omitempty"`
	LastVerifierAt      string         `json:"last_verifier_at,omitempty"`
	LastCleanupDecision string         `json:"last_cleanup_decision,omitempty"`
	LastCleanupReason   string         `json:"last_cleanup_reason,omitempty"`
	LastDryRun          *AttemptRecord `json:"last_dry_run,omitempty"`
	LastFinalize        *AttemptRecord `json:"last_finalize,omitempty"`
}

type WeekState struct {
	WeekKey             string         `json:"week_key"`
	Status              string         `json:"status"`
	CloseAttempts       int            `json:"close_attempts"`
	LastCloseAttemptAt  string         `json:"last_close_attempt_at,omitempty"`
	LastError           string         `json:"last_error,omitempty"`
	SummaryPath         string         `json:"summary_path,omitempty"`
	LastVerifierResult  string         `json:"last_verifier_result,omitempty"`
	LastVerifierAt      string         `json:"last_verifier_at,omitempty"`
	LastCleanupDecision string         `json:"last_cleanup_decision,omitempty"`
	LastCleanupReason   string         `json:"last_cleanup_reason,omitempty"`
	LastDryRun          *AttemptRecord `json:"last_dry_run,omitempty"`
	LastFinalize        *AttemptRecord `json:"last_finalize,omitempty"`
}

type AttemptRecord struct {
	AttemptAt        string `json:"attempt_at"`
	Mode             string `json:"mode"`
	GeneratedPath    string `json:"generated_path,omitempty"`
	VerificationPath string `json:"verification_path,omitempty"`
	VerifierResult   string `json:"verifier_result,omitempty"`
	CleanupDecision  string `json:"cleanup_decision,omitempty"`
	CleanupReason    string `json:"cleanup_reason,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
}

type Store struct {
	path string
}

func NewStore(path string) Store {
	return Store{path: path}
}

func Load(path string) (*FileState, error) {
	return NewStore(path).Load()
}

func (s Store) Load() (*FileState, error) {
	guard, err := filelock.Lock(s.lockPath())
	if err != nil {
		return nil, fmt.Errorf("lock state: %w", err)
	}
	defer guard.Close()
	return loadUnlocked(s.path)
}

func loadUnlocked(path string) (*FileState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &FileState{Days: map[string]*DayState{}, Weeks: map[string]*WeekState{}}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var st FileState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if st.Days == nil {
		st.Days = map[string]*DayState{}
	}
	if st.Weeks == nil {
		st.Weeks = map[string]*WeekState{}
	}
	return &st, nil
}

func (s *FileState) Save(path string) error {
	return NewStore(path).Save(s)
}

func (s Store) Save(st *FileState) error {
	guard, err := filelock.Lock(s.lockPath())
	if err != nil {
		return fmt.Errorf("lock state: %w", err)
	}
	defer guard.Close()
	return saveUnlocked(s.path, st)
}

func (s Store) Update(fn func(*FileState) error) (*FileState, error) {
	guard, err := filelock.Lock(s.lockPath())
	if err != nil {
		return nil, fmt.Errorf("lock state: %w", err)
	}
	defer guard.Close()

	st, err := loadUnlocked(s.path)
	if err != nil {
		return nil, err
	}
	if err := fn(st); err != nil {
		return nil, err
	}
	if err := saveUnlocked(s.path, st); err != nil {
		return nil, err
	}
	return st, nil
}

func saveUnlocked(path string, st *FileState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp state: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("remove old state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func (s Store) lockPath() string {
	return s.path + ".lock"
}

func (s *FileState) EnsureDay(day string) *DayState {
	if existing, ok := s.Days[day]; ok {
		return existing
	}
	ds := &DayState{Date: day, Status: StatusOpen}
	s.Days[day] = ds
	return ds
}

func (s *FileState) MarkCapture(day string, when time.Time) {
	ds := s.EnsureDay(day)
	ds.Status = StatusOpen
	ds.LastCaptureAt = when.UTC().Format(time.RFC3339)
	ds.LastError = ""
}

func (s *FileState) Seal(day string) {
	ds := s.EnsureDay(day)
	if ds.Status == StatusClosed {
		return
	}
	ds.Status = StatusSealed
}

func (s *FileState) MarkClosingAttempt(day string, when time.Time) {
	ds := s.EnsureDay(day)
	ds.CloseAttempts++
	ds.LastCloseAttemptAt = when.UTC().Format(time.RFC3339)
}

func (s *FileState) MarkClosed(day, summaryPath string) {
	ds := s.EnsureDay(day)
	ds.Status = StatusClosed
	ds.LastError = ""
	ds.SummaryPath = summaryPath
	s.LastSuccessfulClose = day
}

func (s *FileState) MarkFailed(day string, err error) {
	ds := s.EnsureDay(day)
	ds.Status = StatusFailed
	ds.LastError = err.Error()
}

func (s *FileState) MarkDayAttempt(day string, record AttemptRecord, dryRun bool) {
	ds := s.EnsureDay(day)
	if dryRun {
		ds.LastDryRun = &record
		return
	}
	ds.LastFinalize = &record
	ds.LastVerifierResult = record.VerifierResult
	ds.LastCleanupDecision = record.CleanupDecision
	ds.LastCleanupReason = record.CleanupReason
	if record.VerifierResult != "" {
		ds.LastVerifierAt = record.AttemptAt
	}
}

func (s *FileState) EnsureWeek(weekKey string) *WeekState {
	if existing, ok := s.Weeks[weekKey]; ok {
		return existing
	}
	ws := &WeekState{WeekKey: weekKey, Status: StatusOpen}
	s.Weeks[weekKey] = ws
	return ws
}

func (s *FileState) MarkWeekClosingAttempt(weekKey string, when time.Time) {
	ws := s.EnsureWeek(weekKey)
	ws.CloseAttempts++
	ws.LastCloseAttemptAt = when.UTC().Format(time.RFC3339)
	if ws.Status != StatusClosed {
		ws.Status = StatusSealed
	}
}

func (s *FileState) MarkWeekClosed(weekKey, summaryPath string) {
	ws := s.EnsureWeek(weekKey)
	ws.Status = StatusClosed
	ws.LastError = ""
	ws.SummaryPath = summaryPath
}

func (s *FileState) MarkWeekFailed(weekKey string, err error) {
	ws := s.EnsureWeek(weekKey)
	ws.Status = StatusFailed
	ws.LastError = err.Error()
}

func (s *FileState) MarkWeekAttempt(weekKey string, record AttemptRecord, dryRun bool) {
	ws := s.EnsureWeek(weekKey)
	if dryRun {
		ws.LastDryRun = &record
		return
	}
	ws.LastFinalize = &record
	ws.LastVerifierResult = record.VerifierResult
	ws.LastCleanupDecision = record.CleanupDecision
	ws.LastCleanupReason = record.CleanupReason
	if record.VerifierResult != "" {
		ws.LastVerifierAt = record.AttemptAt
	}
}

func (s *FileState) ShouldRetryWeek(weekKey string) bool {
	ws, ok := s.Weeks[weekKey]
	if !ok {
		return true
	}
	return ws.Status == StatusOpen || ws.Status == StatusSealed || (ws.Status == StatusFailed && ws.CloseAttempts < 2)
}

func (s *FileState) PendingWeeksBefore(currentWeek string) []string {
	weeks := make([]string, 0, len(s.Weeks))
	for weekKey, ws := range s.Weeks {
		if weekKey >= currentWeek {
			continue
		}
		if ws.Status == StatusClosed {
			continue
		}
		weeks = append(weeks, weekKey)
	}
	sort.Strings(weeks)
	return weeks
}

func (s *FileState) LastCaptureTime(day string) time.Time {
	ds, ok := s.Days[day]
	if !ok || ds.LastCaptureAt == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, ds.LastCaptureAt)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func (s *FileState) ShouldRetry(day string) bool {
	ds, ok := s.Days[day]
	if !ok {
		return false
	}
	return ds.Status == StatusOpen || ds.Status == StatusSealed || (ds.Status == StatusFailed && ds.CloseAttempts < 2)
}

func (s *FileState) PendingBefore(today string) []string {
	dates := make([]string, 0, len(s.Days))
	for day, ds := range s.Days {
		if day >= today {
			continue
		}
		if ds.Status == StatusClosed {
			continue
		}
		dates = append(dates, day)
	}
	sort.Strings(dates)
	return dates
}

func (s *FileState) SetPausedUntil(until time.Time) {
	s.PausedUntil = until.UTC().Format(time.RFC3339)
}

func (s *FileState) ClearPause() {
	s.PausedUntil = ""
}

func (s *FileState) Paused(now time.Time) bool {
	if s.PausedUntil == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, s.PausedUntil)
	if err != nil {
		return false
	}
	return now.UTC().Before(until)
}

func (s *FileState) PausedUntilTime() time.Time {
	if s.PausedUntil == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s.PausedUntil)
	return t
}
