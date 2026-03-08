package dailysummary

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"sauron-sees/internal/audit"
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

type FinalizeOptions struct {
	DryRun bool
}

type Result struct {
	SummaryPath      string
	SheetPaths       []string
	PromptPath       string
	VerificationPath string
	ReportPath       string
	AuditPath        string
	Verification     string
	CleanupEligible  bool
}

func (s Service) FinalizeDay(ctx context.Context, day string, options FinalizeOptions) (Result, error) {
	records, err := metadata.Read(s.Layout.ManifestPath(day))
	if err != nil {
		return Result{}, err
	}
	attempt := audit.New(mode(options.DryRun))
	attempt.InputCount = len(records)

	outputDir := s.Layout.DayRoot(day)
	if options.DryRun {
		outputDir = s.Layout.DayDryRunDir(day)
	}
	sheetsDir := filepath.Join(outputDir, "contact-sheets")
	generatedPath := filepath.Join(outputDir, "generated.md")
	promptPath := filepath.Join(outputDir, "prompt.txt")
	verificationPath := filepath.Join(outputDir, "verification.txt")
	reportPath := filepath.Join(outputDir, "summary.json")
	auditPath := s.Layout.DayAuditPath(day)
	if options.DryRun {
		auditPath = filepath.Join(outputDir, "audit.json")
	}

	sheets, err := contactsheet.Build(day, sheetsDir, records)
	if err != nil {
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, fmt.Errorf("build contact sheets: %w", err)
	}
	prompt, err := s.buildPrompt(day, records)
	if err != nil {
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir output dir: %w", err)
	}
	if err := os.WriteFile(promptPath, []byte(prompt+"\n"), 0o644); err != nil {
		return Result{}, fmt.Errorf("write prompt: %w", err)
	}

	markdown, err := s.Runner.Run(ctx, codex.Request{
		WorkingDir: s.Layout.DayRoot(day),
		Profile:    s.Config.CodexProfile,
		Prompt:     prompt,
		ImagePaths: sheets,
	})
	if err != nil {
		attempt.GeneratedPaths = []string{promptPath}
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, err
	}
	finalMarkdown := strings.TrimSpace(markdown) + "\n"
	if err := os.WriteFile(generatedPath, []byte(finalMarkdown), 0o644); err != nil {
		return Result{}, fmt.Errorf("write generated markdown: %w", err)
	}

	report := qualitygate.Evaluate(qualitygate.KindDaily, finalMarkdown, s.Config.DailySummaryMinWords)
	verification, err := qualitygate.VerifyContent(ctx, s.Runner, s.Config.CodexProfile, s.Layout.DayRoot(day), qualitygate.KindDaily, finalMarkdown)
	if err != nil {
		attempt.GeneratedPaths = []string{promptPath, generatedPath}
		attempt.Validation = report
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, err
	}
	if err := os.WriteFile(verificationPath, []byte(strings.TrimSpace(verification)+"\n"), 0o644); err != nil {
		return Result{}, fmt.Errorf("write verification: %w", err)
	}
	qualitygate.ApplyVerifier(&report, verification)
	if err := qualitygate.WriteJSON(reportPath, report); err != nil {
		return Result{}, fmt.Errorf("write validation report: %w", err)
	}

	result := Result{
		SummaryPath:      generatedPath,
		SheetPaths:       sheets,
		PromptPath:       promptPath,
		VerificationPath: verificationPath,
		ReportPath:       reportPath,
		AuditPath:        auditPath,
		Verification:     verification,
		CleanupEligible:  report.CleanupEligible,
	}

	attempt.GeneratedPaths = []string{promptPath, generatedPath, verificationPath, reportPath}
	attempt.Validation = report
	attempt.VerifierResult = report.VerifierResult
	if report.CleanupEligible {
		attempt.CleanupDecision = "allow"
		attempt.CleanupReason = "all local checks passed and verifier returned SAFE"
	} else {
		attempt.CleanupDecision = "deny"
		attempt.CleanupReason = "validation or verifier gate failed"
	}

	if options.DryRun {
		if err := audit.Write(auditPath, attempt); err != nil {
			return Result{}, err
		}
		return result, nil
	}
	if !report.CleanupEligible {
		if err := audit.Write(auditPath, attempt); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("daily summary failed validation gate")
	}

	if err := os.MkdirAll(s.Layout.DailyMarkdownRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir daily markdown root: %w", err)
	}
	summaryPath := s.Layout.SummaryPath(day)
	if err := os.WriteFile(summaryPath, []byte(finalMarkdown), 0o644); err != nil {
		return Result{}, fmt.Errorf("write summary markdown: %w", err)
	}
	result.SummaryPath = summaryPath
	attempt.GeneratedPaths = append(attempt.GeneratedPaths, summaryPath)
	if s.Config.DeleteAfterSuccess && report.CleanupEligible {
		if err := cleanupDayArtifacts(s.Layout, day); err != nil {
			attempt.CleanupDecision = "deny"
			attempt.CleanupReason = "cleanup failed"
			attempt.ErrorMessage = err.Error()
			_ = audit.Write(auditPath, attempt)
			return Result{}, err
		}
	}
	if err := audit.Write(auditPath, attempt); err != nil {
		return Result{}, err
	}
	return result, nil
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
## Manager Email Draft
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
		Hints          string
	}{
		Date:           day,
		Metadata:       summary,
		Granola:        granola,
		OutputContract: outputContract,
		Timezone:       s.Config.Timezone,
		Hints:          advisoryHints(s.Config.WorkClassification),
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

func advisoryHints(cfg config.WorkClassificationConfig) string {
	if !cfg.AdvisoryHintsEnabled {
		return "No advisory work classification hints were configured."
	}
	var lines []string
	appendList := func(label string, values []string) {
		if len(values) == 0 {
			return
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, strings.Join(values, ", ")))
	}
	appendList("Likely work apps", cfg.IncludeApps)
	appendList("Likely non-work apps", cfg.ExcludeApps)
	appendList("Likely work titles", cfg.IncludeTitles)
	appendList("Likely non-work titles", cfg.ExcludeTitles)
	appendList("Likely work domains", cfg.IncludeDomains)
	appendList("Likely non-work domains", cfg.ExcludeDomains)
	appendList("Additional notes", cfg.Notes)
	if len(lines) == 0 {
		return "No advisory work classification hints were configured."
	}
	return strings.Join(lines, "\n")
}

func mode(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "normal"
}

const defaultPromptTemplate = `
You are preparing a daily work summary for {{ .Date }} in timezone {{ .Timezone }}.

Use the attached contact sheets as the primary evidence of visible work across the day.
Use the metadata summary below as supporting structure:
{{ .Metadata }}

{{ .Granola }}

Advisory work classification hints:
{{ .Hints }}

Requirements:
- Decide yourself which screens represent real work and which are personal, recreational, or irrelevant. Exclude non-work activity from the summary.
- Treat the hints above as advisory only. Override them when the screenshots clearly contradict them.
- Organize the narrative by project, theme, or outcome instead of by app name or work-type category.
- At the end, include a markdown table under "## Work Type Time Breakdown" estimating time by work type. Use categories such as Programming, Modeling / Analysis, Presentations, Meetings, Documents / Writing, Email, Planning / PM, Research / Reference, Design, Terminal / DevOps, Coordination / Chat, and Admin / Operations. Add a category only if it is supported by evidence.
- Do not include a Non-work row in the final table.
- Keep "## Manager Email Draft" directly copyable into an email.
- If the evidence is weak or mixed with non-work browsing, say so explicitly instead of fabricating productivity.
- Be precise and conservative. Do not invent tasks, meetings, or deliverables.
- Mention uncertainty when evidence is weak.
- Write for a manager who needs a concise but accurate daily update.

{{ .OutputContract }}
`
