//go:build windows

package dailysummary

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"sauron-sees/internal/startup"
	"sauron-sees/internal/tray"
)

func windowsChecks() []CheckResult {
	results := []CheckResult{
		check("powershell.exe", func() error { return checkBinary("powershell.exe") }),
		check("schtasks.exe", func() error { return checkBinary("schtasks.exe") }),
		check("tray support", tray.Supported),
		checkStartupTask(),
		checkCodexConfigFile(),
	}
	return results
}

func checkBinary(name string) error {
	_, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return nil
}

func checkStartupTask() CheckResult {
	exists, err := startup.Exists("SauronSeesAgent")
	if err != nil {
		return CheckResult{Name: "startup task", OK: false, Message: err.Error()}
	}
	if exists {
		return CheckResult{Name: "startup task", OK: true, Message: "present"}
	}
	return CheckResult{Name: "startup task", OK: true, Message: "not installed"}
}

func checkCodexConfigFile() CheckResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return CheckResult{Name: "codex config file", OK: false, Message: err.Error()}
	}
	path := filepath.Join(home, ".codex", "config.toml")
	if _, err := os.Stat(path); err != nil {
		return CheckResult{Name: "codex config file", OK: false, Message: err.Error()}
	}
	return CheckResult{Name: "codex config file", OK: true, Message: "ok"}
}
