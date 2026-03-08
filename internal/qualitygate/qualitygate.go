package qualitygate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"sauron-sees/internal/codex"
)

type Kind string

const (
	KindDaily  Kind = "daily"
	KindWeekly Kind = "weekly"
)

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type Report struct {
	Kind            Kind    `json:"kind"`
	WordCount       int     `json:"word_count"`
	MinWords        int     `json:"min_words"`
	VerifierResult  string  `json:"verifier_result"`
	CleanupEligible bool    `json:"cleanup_eligible"`
	Checks          []Check `json:"checks"`
}

func WordCount(text string) int {
	return len(strings.Fields(text))
}

func Evaluate(kind Kind, content string, minWords int) Report {
	trimmed := strings.TrimSpace(content)
	report := Report{
		Kind:      kind,
		WordCount: WordCount(trimmed),
		MinWords:  minWords,
	}
	report.Checks = append(report.Checks, check("non_empty", trimmed != "", "markdown is not empty"))
	report.Checks = append(report.Checks, check("min_words", report.WordCount >= minWords, fmt.Sprintf("word count %d >= %d", report.WordCount, minWords)))
	report.Checks = append(report.Checks, check("yaml_frontmatter", hasFrontmatter(trimmed), "yaml frontmatter delimiters exist"))
	report.Checks = append(report.Checks, check("heading_order", headingsInOrder(trimmed, requiredHeadings(kind)), "required headings exist in order"))
	if kind == KindDaily {
		report.Checks = append(report.Checks, check("work_type_table", hasDailyTable(trimmed), "work type table has at least one data row"))
	}
	report.CleanupEligible = allChecksOK(report.Checks)
	return report
}

func VerifyContent(ctx context.Context, runner codex.Runner, profile string, workingDir string, kind Kind, content string) (string, error) {
	response, err := runner.Run(ctx, codex.Request{
		WorkingDir: workingDir,
		Profile:    profile,
		Prompt:     verifierPrompt(kind, content),
	})
	if err != nil {
		return "", fmt.Errorf("%s verifier failed: %w", kind, err)
	}
	return strings.TrimSpace(response), nil
}

func ApplyVerifier(report *Report, result string) {
	report.VerifierResult = strings.TrimSpace(result)
	report.Checks = append(report.Checks, check("verifier_safe", report.VerifierResult == "SAFE", fmt.Sprintf("verifier returned %q", report.VerifierResult)))
	report.CleanupEligible = allChecksOK(report.Checks)
}

func WriteJSON(path string, report Report) error {
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func verifierPrompt(kind Kind, content string) string {
	return fmt.Sprintf(`You are validating a generated %s markdown.

Return exactly one word:
- SAFE if the markdown is complete, coherent, non-empty, not truncated, has the expected sections, includes a copy-ready manager email draft, and is safe to accept as final.
- UNSAFE if it looks incomplete, malformed, placeholder-like, missing required sections, or suspicious.

Do not explain your reasoning. Output exactly SAFE or UNSAFE.

Markdown:
---
%s
---`, kind, strings.TrimSpace(content))
}

func requiredHeadings(kind Kind) []string {
	switch kind {
	case KindDaily:
		return []string{
			"# Daily Work Summary",
			"## Focus Areas",
			"## Meetings And Decisions",
			"## Concrete Work Done",
			"## Open Threads",
			"## Manager Email Draft",
			"## Work Type Time Breakdown",
		}
	default:
		return []string{
			"# Weekly Work Summary",
			"## Main Focus Areas",
			"## Recurring Projects And Themes",
			"## Meetings And Decisions",
			"## Concrete Progress And Deliverables",
			"## Open Threads And Risks",
			"## Manager Email Draft",
		}
	}
}

func check(name string, ok bool, message string) Check {
	return Check{Name: name, OK: ok, Message: message}
}

func allChecksOK(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

func hasFrontmatter(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return true
		}
	}
	return false
}

func headingsInOrder(content string, headings []string) bool {
	pos := 0
	for _, heading := range headings {
		idx := strings.Index(content[pos:], heading)
		if idx < 0 {
			return false
		}
		pos += idx + len(heading)
	}
	return true
}

func hasDailyTable(content string) bool {
	idx := strings.Index(content, "## Work Type Time Breakdown")
	if idx < 0 {
		return false
	}
	section := content[idx:]
	re := regexp.MustCompile(`(?m)^\|.+\|$`)
	rows := re.FindAllString(section, -1)
	return len(rows) >= 3
}

func filepathDir(path string) string {
	if idx := strings.LastIndex(path, string(os.PathSeparator)); idx >= 0 {
		return path[:idx]
	}
	return "."
}
