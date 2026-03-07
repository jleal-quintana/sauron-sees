package dailysummary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"

	"sauron-sees/internal/codex"
	"sauron-sees/internal/config"
	"sauron-sees/internal/contactsheet"
	"sauron-sees/internal/metadata"
	"sauron-sees/internal/qualitygate"
	"sauron-sees/internal/workspace"
)

type Service struct {
	Config config.Config
	Layout workspace.Layout
	Runner codex.Runner
}

type Result struct {
	SummaryPath string
	SheetPaths  []string
}

func (s Service) FinalizeDay(ctx context.Context, day string) (Result, error) {
	records, err := metadata.Read(s.Layout.ManifestPath(day))
	if err != nil {
		return Result{}, err
	}
	sheets, err := contactsheet.Build(day, s.Layout.SheetsDir(day), records)
	if err != nil {
		return Result{}, fmt.Errorf("build contact sheets: %w", err)
	}
	prompt, err := s.buildPrompt(day, records)
	if err != nil {
		return Result{}, err
	}
	markdown, err := s.Runner.Run(ctx, codex.Request{
		WorkingDir: s.Layout.DayRoot(day),
		Profile:    s.Config.CodexProfile,
		Prompt:     prompt,
		ImagePaths: sheets,
	})
	if err != nil {
		return Result{}, err
	}
	if err := validateMarkdown(markdown); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(s.Layout.DailyMarkdownRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir daily markdown root: %w", err)
	}
	summaryPath := s.Layout.SummaryPath(day)
	finalMarkdown := strings.TrimSpace(markdown) + "\n"
	if err := os.WriteFile(summaryPath, []byte(finalMarkdown), 0o644); err != nil {
		return Result{}, fmt.Errorf("write summary markdown: %w", err)
	}
	if err := qualitygate.VerifyFileAndContent(ctx, s.Runner, s.Config.CodexProfile, s.Layout.DayRoot(day), "daily summary", summaryPath, finalMarkdown, s.Config.DailySummaryMinWords); err != nil {
		return Result{}, err
	}
	if s.Config.DeleteAfterSuccess {
		if err := cleanupDayArtifacts(s.Layout, day); err != nil {
			return Result{}, err
		}
	}
	return Result{
		SummaryPath: summaryPath,
		SheetPaths:  sheets,
	}, nil
}

func (s Service) buildPrompt(day string, records []metadata.CaptureRecord) (string, error) {
	const outputContract = `Return exactly one Markdown document.
Include YAML frontmatter with:
- date
- source
- screenshots_count
- granola_used

Then include these sections with these exact headings:
# Daily Work Summary
## Focus Areas
## Meetings And Decisions
## Concrete Work Done
## Open Threads
## Suggested Manager Update
## Work Type Time Breakdown`

	summary := summarizeMetadata(records, s.Config.CaptureIntervalMinutes)
	granola := "Granola MCP is disabled for this run."
	if s.Config.GranolaEnabled {
		granola = fmt.Sprintf(
			"If the MCP server %q is available, use it to retrieve notes from meetings that happened on %s in local time. Use them only as supporting context. If MCP is unavailable or returns nothing, continue without meetings and explicitly say no meeting context was available.",
			s.Config.GranolaMCPServerName,
			day,
		)
	}

	data := struct {
		Date           string
		Metadata       string
		Granola        string
		OutputContract string
		Timezone       string
	}{
		Date:           day,
		Metadata:       summary,
		Granola:        granola,
		OutputContract: outputContract,
		Timezone:       s.Config.Timezone,
	}

	tmplText := defaultPromptTemplate
	if s.Config.PromptOverridePath != "" {
		content, err := os.ReadFile(s.Config.PromptOverridePath)
		if err != nil {
			return "", fmt.Errorf("read prompt override: %w", err)
		}
		tmplText = string(content)
	}

	tmpl, err := template.New("prompt").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render prompt template: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func validateMarkdown(markdown string) error {
	required := []string{
		"# Daily Work Summary",
		"## Focus Areas",
		"## Meetings And Decisions",
		"## Concrete Work Done",
		"## Open Threads",
		"## Suggested Manager Update",
		"## Work Type Time Breakdown",
	}
	trimmed := strings.TrimSpace(markdown)
	if trimmed == "" {
		return errors.New("daily summary markdown was empty")
	}
	for _, marker := range required {
		if !strings.Contains(trimmed, marker) {
			return fmt.Errorf("daily summary missing required section %q", marker)
		}
	}
	if !strings.HasPrefix(trimmed, "---") {
		return errors.New("daily summary missing YAML frontmatter")
	}
	return nil
}

func cleanupDayArtifacts(layout workspace.Layout, day string) error {
	paths := []string{
		layout.RawDir(day),
		layout.SheetsDir(day),
		layout.ManifestPath(day),
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("cleanup %s: %w", path, err)
		}
	}
	return nil
}

func summarizeMetadata(records []metadata.CaptureRecord, captureIntervalMinutes int) string {
	if len(records) == 0 {
		return "No screenshots were captured for this day. If evidence is missing, say so explicitly."
	}
	processCounts := map[string]int{}
	windowCounts := map[string]int{}
	monitorCounts := map[int]int{}
	first := records[0].Time()
	last := records[len(records)-1].Time()
	for _, rec := range records {
		processCounts[blankFallback(rec.ActiveProcess, "unknown")]++
		windowCounts[blankFallback(rec.ActiveWindowTitle, "unknown")]++
		monitorCounts[rec.MonitorCount]++
	}

	lines := []string{
		fmt.Sprintf("Captured %d screenshots at roughly %d-minute intervals.", len(records), captureIntervalMinutes),
		fmt.Sprintf("First capture: %s UTC.", first.Format(time.RFC3339)),
		fmt.Sprintf("Last capture: %s UTC.", last.Format(time.RFC3339)),
	}

	hours := contactsheet.HourBuckets(records)
	hourKeys := make([]string, 0, len(hours))
	for k := range hours {
		hourKeys = append(hourKeys, k)
	}
	sort.Strings(hourKeys)
	for _, hour := range hourKeys {
		lines = append(lines, fmt.Sprintf("Hour %s: %d screenshots.", hour, hours[hour]))
	}
	for monitors, count := range monitorCounts {
		lines = append(lines, fmt.Sprintf("Monitor count %d appeared in %d captures.", monitors, count))
	}
	for _, item := range topCounts(processCounts, 8) {
		lines = append(lines, fmt.Sprintf("Process %s: %d captures.", item.Key, item.Count))
	}
	for _, item := range topCounts(windowCounts, 8) {
		lines = append(lines, fmt.Sprintf("Window %s: %d captures.", item.Key, item.Count))
	}
	return strings.Join(lines, "\n")
}

type countItem struct {
	Key   string
	Count int
}

func topCounts(values map[string]int, max int) []countItem {
	items := make([]countItem, 0, len(values))
	for key, count := range values {
		items = append(items, countItem{Key: key, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Key < items[j].Key
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > max {
		items = items[:max]
	}
	return items
}

func blankFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

const defaultPromptTemplate = `
You are preparing a daily work summary for {{ .Date }} in timezone {{ .Timezone }}.

Use the attached contact sheets as the primary evidence of visible work across the day.
Use the metadata summary below as supporting structure:
{{ .Metadata }}

{{ .Granola }}

Requirements:
- Decide yourself which screens represent real work and which are personal, recreational, or irrelevant. Exclude non-work activity from the summary.
- Organize the narrative by project, theme, or outcome instead of by app name or work-type category.
- At the end, include a markdown table under "## Work Type Time Breakdown" estimating time by work type. Use categories such as Programming, Modeling / Analysis, Presentations, Meetings, Documents / Writing, Email, Planning / PM, Research / Reference, Design, Terminal / DevOps, Coordination / Chat, and Admin / Operations. Add a category only if it is supported by evidence.
- Do not include a Non-work row in the final table.
- If the evidence is weak or mixed with non-work browsing, say so explicitly instead of fabricating productivity.
- Be precise and conservative. Do not invent tasks, meetings, or deliverables.
- Mention uncertainty when evidence is weak.
- Write for a manager who needs a concise but accurate daily update.

{{ .OutputContract }}
`
