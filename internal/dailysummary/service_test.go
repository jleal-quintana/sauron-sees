package dailysummary

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sauron-sees/internal/codex"
	"sauron-sees/internal/config"
	"sauron-sees/internal/metadata"
	"sauron-sees/internal/workspace"
)

type fakeRunner struct {
	markdown string
	err      error
	lastReq  codex.Request
}

func (f *fakeRunner) Run(ctx context.Context, req codex.Request) (string, error) {
	f.lastReq = req
	if strings.Contains(req.Prompt, "Return exactly one word:") {
		return "SAFE", nil
	}
	return f.markdown, f.err
}

func (f *fakeRunner) CheckBinary() error                { return nil }
func (f *fakeRunner) CheckProfile(profile string) error { return nil }
func (f *fakeRunner) CheckMCPServer(name string) error  { return nil }

func TestFinalizeDayDeletesArtifactsOnSuccess(t *testing.T) {
	layout, cfg, day := prepareDayFixture(t)
	runner := &fakeRunner{markdown: validMarkdown(day)}
	service := Service{Config: cfg, Layout: layout, Runner: runner}

	result, err := service.FinalizeDay(context.Background(), day, FinalizeOptions{})
	if err != nil {
		t.Fatalf("FinalizeDay() error = %v", err)
	}
	if result.SummaryPath == "" {
		t.Fatalf("expected summary path")
	}
	if _, err := os.Stat(result.SummaryPath); err != nil {
		t.Fatalf("expected summary file: %v", err)
	}
	if _, err := os.Stat(layout.ManifestPath(day)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected manifest to be deleted, got %v", err)
	}
	if _, err := os.Stat(layout.DayAuditPath(day)); err != nil {
		t.Fatalf("expected audit file: %v", err)
	}
}

func TestFinalizeDayDryRunPreservesArtifacts(t *testing.T) {
	layout, cfg, day := prepareDayFixture(t)
	runner := &fakeRunner{markdown: validMarkdown(day)}
	service := Service{Config: cfg, Layout: layout, Runner: runner}

	result, err := service.FinalizeDay(context.Background(), day, FinalizeOptions{DryRun: true})
	if err != nil {
		t.Fatalf("FinalizeDay() error = %v", err)
	}
	if !strings.Contains(result.SummaryPath, "dry-run") {
		t.Fatalf("expected dry-run summary path, got %s", result.SummaryPath)
	}
	if _, err := os.Stat(layout.ManifestPath(day)); err != nil {
		t.Fatalf("expected manifest to remain after dry-run: %v", err)
	}
}

func TestFinalizeDayPreservesArtifactsOnInvalidMarkdown(t *testing.T) {
	layout, cfg, day := prepareDayFixture(t)
	runner := &fakeRunner{markdown: "# wrong"}
	service := Service{Config: cfg, Layout: layout, Runner: runner}

	if _, err := service.FinalizeDay(context.Background(), day, FinalizeOptions{}); err == nil {
		t.Fatalf("expected validation error")
	}
	if _, err := os.Stat(layout.ManifestPath(day)); err != nil {
		t.Fatalf("expected manifest to remain after failure: %v", err)
	}
}

func prepareDayFixture(t *testing.T) (workspace.Layout, config.Config, string) {
	t.Helper()
	root := t.TempDir()
	day := "2026-03-09"
	layout := workspace.Layout{
		TempRoot:           filepath.Join(root, "temp"),
		DailyMarkdownRoot:  filepath.Join(root, "daily"),
		WeeklyMarkdownRoot: filepath.Join(root, "weekly"),
	}
	cfg := config.Default()
	cfg.TempRoot = layout.TempRoot
	cfg.DailyMarkdownRoot = layout.DailyMarkdownRoot
	cfg.WeeklyMarkdownRoot = layout.WeeklyMarkdownRoot
	cfg.DailySummaryMinWords = 50

	imagePath := filepath.Join(layout.RawDir(day), "shot.jpg")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatalf("mkdir raw dir: %v", err)
	}
	if err := writeJPEG(imagePath); err != nil {
		t.Fatalf("write image: %v", err)
	}
	record := metadata.CaptureRecord{
		Timestamp:         time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ImagePath:         imagePath,
		MonitorCount:      2,
		ActiveWindowTitle: "editor",
		ActiveProcess:     "code.exe",
	}
	if err := metadata.Append(layout.ManifestPath(day), record); err != nil {
		t.Fatalf("append manifest: %v", err)
	}
	return layout, cfg, day
}

func validMarkdown(day string) string {
	return strings.TrimSpace(`
---
date: ` + day + `
source: sauron-sees
screenshots_count: 1
granola_used: false
---
# Daily Work Summary
## Focus Areas
Worked on the MVP.
## Meetings And Decisions
No meeting context was available.
## Concrete Work Done
Implemented the capture and summary pipeline.
## Open Threads
Need Windows validation.
## Manager Email Draft
Implemented the first end-to-end version of the tool.
## Work Type Time Breakdown
| Category | Estimated Time | Samples |
| --- | ---: | ---: |
| Programming | 1h 00m | 12 |
`)
}

func writeJPEG(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 800, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 800; x++ {
			img.SetRGBA(x, y, color.RGBA{50, 120, 200, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 80})
}
