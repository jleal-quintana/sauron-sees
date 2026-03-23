package metadata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type CaptureRecord struct {
	Timestamp         string `json:"timestamp"`
	ImagePath         string `json:"image_path"`
	MonitorCount      int    `json:"monitor_count"`
	ActiveWindowTitle string `json:"active_window_title"`
	ActiveProcess     string `json:"active_process"`
	SessionLocked     bool   `json:"session_locked"`
}

func (r CaptureRecord) Time() time.Time {
	ts, _ := time.Parse(time.RFC3339, r.Timestamp)
	return ts
}

func (r CaptureRecord) TimeIn(location *time.Location) time.Time {
	ts := r.Time()
	if ts.IsZero() || location == nil {
		return ts
	}
	return ts.In(location)
}

func Append(path string, record CaptureRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir manifest dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal manifest record: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append manifest record: %w", err)
	}
	return nil
}

func Read(path string) ([]CaptureRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var records []CaptureRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec CaptureRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan manifest: %w", err)
	}
	return records, nil
}
