package weeklysummary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sauron-sees/internal/codex"
	"sauron-sees/internal/config"
	"sauron-sees/internal/workspace"
)

type fakeRunner struct {
	markdown string
}

func (f *fakeRunner) Run(ctx context.Context, req codex.Request) (string, error) {
	if strings.Contains(req.Prompt, "Return exactly one word:") {
		return "SAFE", nil
	}
	return f.markdown, nil
}

func (f *fakeRunner) CheckBinary() error                { return nil }
func (f *fakeRunner) CheckProfile(profile string) error { return nil }
func (f *fakeRunner) CheckMCPServer(name string) error  { return nil }

func TestFinalizeWeekWritesMarkdown(t *testing.T) {
	root := t.TempDir()
	layout := workspace.Layout{
		TempRoot:           filepath.Join(root, "temp"),
		DailyMarkdownRoot:  filepath.Join(root, "daily"),
		WeeklyMarkdownRoot: filepath.Join(root, "weekly"),
	}
	cfg := config.Default()
	cfg.DailyMarkdownRoot = layout.DailyMarkdownRoot
	cfg.WeeklyMarkdownRoot = layout.WeeklyMarkdownRoot
	cfg.WeeklySummaryMinWords = 50

	if err := os.MkdirAll(layout.DailyMarkdownRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.SummaryPath("2026-03-09"), []byte(validDaily), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := Service{
		Config: cfg,
		Layout: layout,
		Runner: &fakeRunner{markdown: validWeekly},
	}
	start := mustDate("2026-03-09")
	end := mustDate("2026-03-15")
	result, err := svc.FinalizeWeek(context.Background(), "2026-W11", start, end)
	if err != nil {
		t.Fatalf("FinalizeWeek() error = %v", err)
	}
	if _, err := os.Stat(result.SummaryPath); err != nil {
		t.Fatalf("expected weekly file: %v", err)
	}
}

func mustDate(day string) (outTime time.Time) {
	outTime, _ = time.Parse("2006-01-02", day)
	return
}

const validDaily = `---
date: 2026-03-09
source: sauron-sees
screenshots_count: 5
granola_used: false
---
# Daily Work Summary
## Focus Areas
Worked on the project.
## Meetings And Decisions
No meetings.
## Concrete Work Done
Implemented weekly support.
## Open Threads
Need verification.
## Suggested Manager Update
Built the weekly flow.
## Work Type Time Breakdown
| Category | Estimated Time | Samples |
| --- | ---: | ---: |
| Programming | 1h 00m | 2 |
`

const validWeekly = `---
week: 2026-W11
start_date: 2026-03-09
end_date: 2026-03-15
source: sauron-sees
---
# Weekly Work Summary
## Main Focus Areas
Focused on building the MVP automation.
## Recurring Projects And Themes
Repeated work around summaries and verification.
## Meetings And Decisions
No notable meetings were captured.
## Concrete Progress And Deliverables
Implemented the weekly command and verification flow.
## Open Threads And Risks
Need Windows validation for runtime behavior.
## Manager Email Draft
This week I completed the first end-to-end automation flow and added verification before cleanup.
`
