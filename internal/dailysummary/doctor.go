package dailysummary

import (
	"fmt"
	"os"

	"sauron-sees/internal/codex"
	"sauron-sees/internal/config"
)

type CheckResult struct {
	Name    string
	OK      bool
	Message string
}

func Doctor(cfg config.Config, runner codex.Runner) []CheckResult {
	results := []CheckResult{
		check("codex binary", runner.CheckBinary),
		checkProfile(cfg, runner),
		checkWritableDir("daily markdown root", cfg.DailyMarkdownRoot),
		checkWritableDir("weekly markdown root", cfg.WeeklyMarkdownRoot),
		checkEnsureDir("temp root", cfg.TempRoot),
	}
	results = append(results, windowsChecks()...)
	if cfg.GranolaEnabled {
		results = append(results, check(fmt.Sprintf("granola MCP (%s)", cfg.GranolaMCPServerName), func() error {
			return runner.CheckMCPServer(cfg.GranolaMCPServerName)
		}))
	}
	return results
}

func HasBlockingIssue(results []CheckResult) bool {
	for _, result := range results {
		if !result.OK && (result.Name == "codex binary" || result.Name == "codex profile") {
			return true
		}
	}
	return false
}

func check(name string, fn func() error) CheckResult {
	if err := fn(); err != nil {
		return CheckResult{Name: name, OK: false, Message: err.Error()}
	}
	return CheckResult{Name: name, OK: true, Message: "ok"}
}

func checkProfile(cfg config.Config, runner codex.Runner) CheckResult {
	if err := runner.CheckProfile(cfg.CodexProfile); err != nil {
		return CheckResult{Name: "codex profile", OK: false, Message: err.Error()}
	}
	return CheckResult{Name: "codex profile", OK: true, Message: "ok"}
}

func checkEnsureDir(name string, path string) CheckResult {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return CheckResult{Name: name, OK: false, Message: err.Error()}
	}
	return CheckResult{Name: name, OK: true, Message: "ok"}
}

func checkWritableDir(name string, path string) CheckResult {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return CheckResult{Name: name, OK: false, Message: err.Error()}
	}
	f, err := os.CreateTemp(path, "write-check-*.tmp")
	if err != nil {
		return CheckResult{Name: name, OK: false, Message: err.Error()}
	}
	nameOnDisk := f.Name()
	f.Close()
	_ = os.Remove(nameOnDisk)
	return CheckResult{Name: name, OK: true, Message: "ok"}
}
