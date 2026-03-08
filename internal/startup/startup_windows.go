//go:build windows

package startup

import (
	"fmt"
	"os/exec"
	"strings"
)

func Install(taskName string, executable string, configPath string) error {
	target := fmt.Sprintf(`"%s" agent`, executable)
	if configPath != "" {
		target = fmt.Sprintf(`"%s" --config "%s" agent`, executable, configPath)
	}
	cmd := exec.Command(
		"schtasks.exe",
		"/Create",
		"/F",
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/TN", taskName,
		"/TR", target,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create scheduled task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Uninstall(taskName string) error {
	cmd := exec.Command("schtasks.exe", "/Delete", "/F", "/TN", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete scheduled task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Exists(taskName string) (bool, error) {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(strings.ToLower(text), "cannot find") || strings.Contains(strings.ToLower(text), "no se puede encontrar") {
			return false, nil
		}
		return false, fmt.Errorf("query scheduled task: %w: %s", err, text)
	}
	return true, nil
}
