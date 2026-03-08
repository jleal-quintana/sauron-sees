package weeklysummary

import (
	"bytes"
	"context"
	"errors"
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
	"sauron-sees/internal/qualitygate"
	"sauron-sees/internal/workspace"
)

type Service struct {
	Config config.Config
	Layout workspace.Layout
	Runner codex.Runner
}

type DailyDocument struct {
	Date    string
	Path    string
	Content string
}

type FinalizeOptions struct {
	DryRun bool
}

type Result struct {
	SummaryPath      string
	PromptPath       string
	VerificationPath string
	ReportPath       string
	AuditPath        string
	WeekKey          string
	Verification     string
	CleanupEligible  bool
}

func (s Service) FinalizeWeek(ctx context.Context, weekKey string, start time.Time, end time.Time, options FinalizeOptions) (Result, error) {
	docs, err := s.collectDocs(start, end)
	if err != nil {
		return Result{}, err
	}
	attempt := audit.New(mode(options.DryRun))
	attempt.InputCount = len(docs)

	outputDir := s.Layout.WeeklyRoot(weekKey)
	if options.DryRun {
		outputDir = s.Layout.WeeklyDryRunDir(weekKey)
	}
	generatedPath := filepath.Join(outputDir, "generated.md")
	promptPath := filepath.Join(outputDir, "prompt.txt")
	verificationPath := filepath.Join(outputDir, "verification.txt")
	reportPath := filepath.Join(outputDir, "summary.json")
	auditPath := s.Layout.WeeklyAuditPath(weekKey)
	if options.DryRun {
		auditPath = filepath.Join(outputDir, "audit.json")
	}

	prompt, err := s.buildPrompt(weekKey, start, end, docs)
	if err != nil {
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir weekly output dir: %w", err)
	}
	if err := os.WriteFile(promptPath, []byte(prompt+"\n"), 0o644); err != nil {
		return Result{}, fmt.Errorf("write weekly prompt: %w", err)
	}

	markdown, err := s.Runner.Run(ctx, codex.Request{
		WorkingDir: s.Layout.WeeklyMarkdownRoot,
		Profile:    s.Config.CodexProfile,
		Prompt:     prompt,
	})
	if err != nil {
		attempt.GeneratedPaths = []string{promptPath}
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, err
	}
	finalMarkdown := strings.TrimSpace(markdown) + "\n"
	if err := os.WriteFile(generatedPath, []byte(finalMarkdown), 0o644); err != nil {
		return Result{}, fmt.Errorf("write weekly generated markdown: %w", err)
	}

	report := qualitygate.Evaluate(qualitygate.KindWeekly, finalMarkdown, s.Config.WeeklySummaryMinWords)
	verification, err := qualitygate.VerifyContent(ctx, s.Runner, s.Config.CodexProfile, s.Layout.WeeklyMarkdownRoot, qualitygate.KindWeekly, finalMarkdown)
	if err != nil {
		attempt.GeneratedPaths = []string{promptPath, generatedPath}
		attempt.Validation = report
		attempt.ErrorMessage = err.Error()
		_ = audit.Write(auditPath, attempt)
		return Result{}, err
	}
	if err := os.WriteFile(verificationPath, []byte(strings.TrimSpace(verification)+"\n"), 0o644); err != nil {
		return Result{}, fmt.Errorf("write weekly verification: %w", err)
	}
	qualitygate.ApplyVerifier(&report, verification)
	if err := qualitygate.WriteJSON(reportPath, report); err != nil {
		return Result{}, fmt.Errorf("write weekly validation report: %w", err)
	}

	result := Result{
		SummaryPath:      generatedPath,
		PromptPath:       promptPath,
		VerificationPath: verificationPath,
		ReportPath:       reportPath,
		AuditPath:        auditPath,
		WeekKey:          weekKey,
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
		return Result{}, fmt.Errorf("weekly summary failed validation gate")
	}

	if err := os.MkdirAll(s.Layout.WeeklyMarkdownRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir weekly markdown root: %w", err)
	}
	summaryPath := s.Layout.WeeklySummaryPath(weekKey)
	if err := os.WriteFile(summaryPath, []byte(finalMarkdown), 0o644); err != nil {
		return Result{}, fmt.Errorf("write weekly summary: %w", err)
	}
	result.SummaryPath = summaryPath
	attempt.GeneratedPaths = append(attempt.GeneratedPaths, summaryPath)
	if err := audit.Write(auditPath, attempt); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s Service) collectDocs(start time.Time, end time.Time) ([]DailyDocument, error) {
	var docs []DailyDocument
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		path := s.Layout.SummaryPath(date)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read daily summary %s: %w", path, err)
		}
		docs = append(docs, DailyDocument{
			Date:    date,
			Path:    path,
			Content: string(data),
		})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Date < docs[j].Date })
	return docs, nil
}

func (s Service) buildPrompt(weekKey string, start time.Time, end time.Time, docs []DailyDocument) (string, error) {
	data := struct {
		WeekKey string
		Start   string
		End     string
		Corpus  string
	}{
		WeekKey: weekKey,
		Start:   start.Format("2006-01-02"),
		End:     end.Format("2006-01-02"),
		Corpus:  joinDocs(docs),
	}
	tmpl, err := template.New("weekly").Parse(defaultPromptTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func joinDocs(docs []DailyDocument) string {
	if len(docs) == 0 {
		return "No daily summaries were found for this range."
	}
	parts := make([]string, 0, len(docs))
	for _, doc := range docs {
		parts = append(parts, fmt.Sprintf("File: %s\nDate: %s\n%s", filepath.Base(doc.Path), doc.Date, strings.TrimSpace(doc.Content)))
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func mode(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "normal"
}

const defaultPromptTemplate = `
You are producing a weekly work summary for ISO week {{ .WeekKey }} covering {{ .Start }} to {{ .End }}.

Source corpus:
{{ .Corpus }}

Requirements:
- Use only the daily markdowns above.
- Prefer cross-day themes, projects, and deliverables over a day-by-day retelling.
- Call out missing days if the corpus is incomplete.
- Keep the writing factual and manager-ready.
- The "## Manager Email Draft" section should be directly copyable into an email.

Return exactly one Markdown document.
Include YAML frontmatter with:
- week
- start_date
- end_date
- source

Then include these exact headings:
# Weekly Work Summary
## Main Focus Areas
## Recurring Projects And Themes
## Meetings And Decisions
## Concrete Progress And Deliverables
## Open Threads And Risks
## Manager Email Draft
`
