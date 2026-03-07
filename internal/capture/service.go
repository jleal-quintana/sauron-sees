package capture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sauron-sees/internal/metadata"
	"sauron-sees/internal/platform"
	"sauron-sees/internal/workspace"
)

var ErrSessionLocked = errors.New("session is locked")

type Service struct {
	Host        platform.Host
	Layout      workspace.Layout
	ImageMaxDim int
	JPEGQuality int
}

func (s Service) Capture(now time.Time, day string) (metadata.CaptureRecord, error) {
	meta, err := s.Host.DesktopMetadata()
	if err != nil {
		return metadata.CaptureRecord{}, fmt.Errorf("desktop metadata: %w", err)
	}
	if meta.SessionLocked {
		return metadata.CaptureRecord{}, ErrSessionLocked
	}

	if err := os.MkdirAll(s.Layout.RawDir(day), 0o755); err != nil {
		return metadata.CaptureRecord{}, fmt.Errorf("mkdir raw dir: %w", err)
	}

	filename := now.Format("150405") + "-" + fmt.Sprintf("%09d", now.Nanosecond()) + ".jpg"
	path := filepath.Join(s.Layout.RawDir(day), filename)
	if err := s.Host.CaptureCompositeJPEG(path, s.ImageMaxDim, s.JPEGQuality); err != nil {
		return metadata.CaptureRecord{}, fmt.Errorf("capture screen: %w", err)
	}

	record := metadata.CaptureRecord{
		Timestamp:         now.UTC().Format(time.RFC3339),
		ImagePath:         path,
		MonitorCount:      meta.MonitorCount,
		ActiveWindowTitle: meta.ActiveWindowTitle,
		ActiveProcess:     meta.ActiveProcess,
		SessionLocked:     false,
	}
	if err := metadata.Append(s.Layout.ManifestPath(day), record); err != nil {
		return metadata.CaptureRecord{}, err
	}
	return record, nil
}
