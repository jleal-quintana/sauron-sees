package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Request struct {
	WorkingDir string
	Profile    string
	Prompt     string
	ImagePaths []string
}

type Runner interface {
	Run(ctx context.Context, req Request) (string, error)
	CheckBinary() error
	CheckProfile(profile string) error
	CheckMCPServer(name string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, req Request) (string, error) {
	if req.Profile == "" {
		return "", errors.New("codex profile must not be empty")
	}
	tmpFile, err := os.CreateTemp("", "sauron-sees-codex-*.md")
	if err != nil {
		return "", fmt.Errorf("create codex output file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := []string{
		"exec",
		"--profile", req.Profile,
		"--skip-git-repo-check",
		"--output-last-message", tmpPath,
	}
	for _, imagePath := range req.ImagePaths {
		args = append(args, "--image", imagePath)
	}
	if req.WorkingDir != "" {
		args = append(args, "-C", req.WorkingDir)
	}
	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, "codex", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("codex exec failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read codex output: %w", err)
	}
	message := strings.TrimSpace(string(data))
	if message == "" {
		return "", errors.New("codex output was empty")
	}
	return message, nil
}

func (ExecRunner) CheckBinary() error {
	_, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex binary not found in PATH")
	}
	return nil
}

func (ExecRunner) CheckProfile(profile string) error {
	configDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	path := filepath.Join(configDir, ".codex", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read codex config: %w", err)
	}
	markers := []string{
		fmt.Sprintf("[profiles.%s]", profile),
		fmt.Sprintf("[profile.%s]", profile),
		fmt.Sprintf("profile = %q", profile),
	}
	for _, marker := range markers {
		if bytes.Contains(data, []byte(marker)) {
			return nil
		}
	}
	return fmt.Errorf("codex profile %q not found in %s", profile, path)
}

func (ExecRunner) CheckMCPServer(name string) error {
	if name == "" {
		return nil
	}
	cmd := exec.Command("codex", "mcp", "get", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codex mcp get %q failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}
