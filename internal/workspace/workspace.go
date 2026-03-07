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

func (l Layout) ManifestPath(day string) string {
	return filepath.Join(l.DayRoot(day), "manifest.jsonl")
}

func (l Layout) StatePath() string {
	return filepath.Join(l.TempRoot, "state.json")
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
