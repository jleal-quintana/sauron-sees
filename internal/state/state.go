package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	StatusOpen   = "open"
	StatusSealed = "sealed"
	StatusClosed = "closed"
	StatusFailed = "failed"
)

type FileState struct {
	LastSuccessfulClose string                `json:"last_successful_close"`
	Days                map[string]*DayState  `json:"days"`
	Weeks               map[string]*WeekState `json:"weeks"`
}

type DayState struct {
	Date               string `json:"date"`
	Status             string `json:"status"`
	LastCaptureAt      string `json:"last_capture_at,omitempty"`
	CloseAttempts      int    `json:"close_attempts"`
	LastCloseAttemptAt string `json:"last_close_attempt_at,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	SummaryPath        string `json:"summary_path,omitempty"`
}

type WeekState struct {
	WeekKey            string `json:"week_key"`
	Status             string `json:"status"`
	CloseAttempts      int    `json:"close_attempts"`
	LastCloseAttemptAt string `json:"last_close_attempt_at,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	SummaryPath        string `json:"summary_path,omitempty"`
}

func Load(path string) (*FileState, error) {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
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
