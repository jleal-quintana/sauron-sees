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

type Result struct {
	SummaryPath string
	WeekKey     string
}

func (s Service) FinalizeWeek(ctx context.Context, weekKey string, start time.Time, end time.Time) (Result, error) {
	docs, err := s.collectDocs(start, end)
	if err != nil {
		return Result{}, err
	}
	prompt, err := s.buildPrompt(weekKey, start, end, docs)
	if err != nil {
		return Result{}, err
	}
	markdown, err := s.Runner.Run(ctx, codex.Request{
		WorkingDir: s.Layout.WeeklyMarkdownRoot,
		Profile:    s.Config.CodexProfile,
		Prompt:     prompt,
	})
	if err != nil {
		return Result{}, err
	}
	if err := validateMarkdown(markdown); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(s.Layout.WeeklyMarkdownRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir weekly markdown root: %w", err)
	}
	summaryPath := s.Layout.WeeklySummaryPath(weekKey)
	finalMarkdown := strings.TrimSpace(markdown) + "\n"
	if err := os.WriteFile(summaryPath, []byte(finalMarkdown), 0o644); err != nil {
		return Result{}, fmt.Errorf("write weekly summary: %w", err)
	}
	if err := qualitygate.VerifyFileAndContent(ctx, s.Runner, s.Config.CodexProfile, s.Layout.WeeklyMarkdownRoot, "weekly summary", summaryPath, finalMarkdown, s.Config.WeeklySummaryMinWords); err != nil {
		return Result{}, err
	}
	return Result{SummaryPath: summaryPath, WeekKey: weekKey}, nil
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

func validateMarkdown(markdown string) error {
	required := []string{
		"# Weekly Work Summary",
		"## Main Focus Areas",
		"## Recurring Projects And Themes",
		"## Meetings And Decisions",
		"## Concrete Progress And Deliverables",
		"## Open Threads And Risks",
		"## Manager Email Draft",
	}
	trimmed := strings.TrimSpace(markdown)
	if trimmed == "" {
		return errors.New("weekly summary markdown was empty")
	}
	for _, marker := range required {
		if !strings.Contains(trimmed, marker) {
			return fmt.Errorf("weekly summary missing required section %q", marker)
		}
	}
	if !strings.HasPrefix(trimmed, "---") {
		return errors.New("weekly summary missing YAML frontmatter")
	}
	return nil
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
