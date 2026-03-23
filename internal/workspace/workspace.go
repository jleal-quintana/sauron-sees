package workspace

import "path/filepath"

type Layout struct {
	TempRoot           string
	DailyMarkdownRoot  string
	WeeklyMarkdownRoot string
}

func (l Layout) DayRoot(day string) string {
	return filepath.Join(l.TempRoot, day)
}

func (l Layout) RawDir(day string) string {
	return filepath.Join(l.DayRoot(day), "raw")
}

func (l Layout) SheetsDir(day string) string {
	return filepath.Join(l.DayRoot(day), "sheets")
}

func (l Layout) DayDryRunDir(day string) string {
	return filepath.Join(l.DayRoot(day), "dry-run")
}

func (l Layout) ManifestPath(day string) string {
	return filepath.Join(l.DayRoot(day), "manifest.jsonl")
}

func (l Layout) DayAuditPath(day string) string {
	return filepath.Join(l.DayRoot(day), "audit.json")
}

func (l Layout) StatePath() string {
	return filepath.Join(l.TempRoot, "state.json")
}

func (l Layout) PIDPath() string {
	return filepath.Join(l.TempRoot, "agent.pid")
}

func (l Layout) StopPath() string {
	return filepath.Join(l.TempRoot, "agent.stop")
}

func (l Layout) LogPath() string {
	return filepath.Join(l.TempRoot, "logs", "agent.log")
}

func (l Layout) SummaryPath(day string) string {
	return filepath.Join(l.DailyMarkdownRoot, day+"-work-summary.md")
}

func (l Layout) WeeklySummaryPath(weekKey string) string {
	return filepath.Join(l.WeeklyMarkdownRoot, weekKey+"-work-summary.md")
}

func (l Layout) WeeklyRoot(weekKey string) string {
	return filepath.Join(l.TempRoot, "weekly", weekKey)
}

func (l Layout) WeeklyDryRunDir(weekKey string) string {
	return filepath.Join(l.WeeklyRoot(weekKey), "dry-run")
}

func (l Layout) WeeklyAuditPath(weekKey string) string {
	return filepath.Join(l.WeeklyRoot(weekKey), "audit.json")
}
