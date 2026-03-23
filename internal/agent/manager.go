package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sauron-sees/internal/filelock"
)

type Manager struct {
	pidPath  string
	stopPath string
}

type Lease struct {
	manager Manager
	lock    *filelock.Guard
	pid     int
}

type AlreadyRunningError struct {
	PID int
}

type pidRecord struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

func NewManager(pidPath string, stopPath string) Manager {
	return Manager{pidPath: pidPath, stopPath: stopPath}
}

func (e AlreadyRunningError) Error() string {
	return fmt.Sprintf("agent already running with pid %d", e.PID)
}

func (m Manager) PIDPath() string {
	return m.pidPath
}

func (m Manager) StopPath() string {
	return m.stopPath
}

func (m Manager) Acquire() (*Lease, error) {
	lock, err := filelock.TryLock(m.pidPath)
	if err != nil {
		if errors.Is(err, filelock.ErrUnavailable) {
			pid, readErr := m.ReadPID()
			if readErr != nil {
				return nil, fmt.Errorf("agent already running")
			}
			return nil, AlreadyRunningError{PID: pid}
		}
		return nil, fmt.Errorf("lock pid file: %w", err)
	}

	lease := &Lease{
		manager: m,
		lock:    lock,
		pid:     os.Getpid(),
	}
	if err := lease.writePID(); err != nil {
		lock.Close()
		return nil, err
	}
	if err := os.Remove(m.stopPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		lock.Close()
		return nil, fmt.Errorf("clear stop request: %w", err)
	}
	return lease, nil
}

func (m Manager) ReadPID() (int, error) {
	data, err := os.ReadFile(m.pidPath)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	return parsePID(data)
}

func (m Manager) RequestStop(pid int) error {
	if err := os.MkdirAll(filepath.Dir(m.stopPath), 0o755); err != nil {
		return fmt.Errorf("mkdir stop dir: %w", err)
	}
	record := pidRecord{
		PID:       pid,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal stop request: %w", err)
	}
	if err := os.WriteFile(m.stopPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write stop request: %w", err)
	}
	return nil
}

func (m Manager) StopRequested() (bool, error) {
	_, err := os.Stat(m.stopPath)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat stop request: %w", err)
}

func (m Manager) ClearStopRequest() error {
	if err := os.Remove(m.stopPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stop request: %w", err)
	}
	return nil
}

func (l *Lease) PID() int {
	return l.pid
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	if err := l.manager.ClearStopRequest(); err != nil {
		return err
	}
	if l.lock != nil && l.lock.File() != nil {
		if err := l.lock.File().Truncate(0); err != nil {
			return fmt.Errorf("truncate pid file: %w", err)
		}
		if _, err := l.lock.File().Seek(0, 0); err != nil {
			return fmt.Errorf("rewind pid file: %w", err)
		}
	}
	if err := l.lock.Close(); err != nil {
		return err
	}
	if err := os.Remove(l.manager.pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pid file: %w", err)
	}
	return nil
}

func (l *Lease) writePID() error {
	record := pidRecord{
		PID:       l.pid,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal pid file: %w", err)
	}
	file := l.lock.File()
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate pid file: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("rewind pid file: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

func parsePID(data []byte) (int, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0, errors.New("pid file is empty")
	}
	var record pidRecord
	if err := json.Unmarshal([]byte(trimmed), &record); err == nil && record.PID > 0 {
		return record.PID, nil
	}
	pid, err := strconv.Atoi(trimmed)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("parse pid file")
	}
	return pid, nil
}
