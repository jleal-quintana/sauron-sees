package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sauron-sees/internal/qualitygate"
)

type Attempt struct {
	AttemptedAt     string             `json:"attempted_at"`
	Mode            string             `json:"mode"`
	InputCount      int                `json:"input_count"`
	GeneratedPaths  []string           `json:"generated_paths"`
	Validation      qualitygate.Report `json:"validation"`
	VerifierResult  string             `json:"verifier_result"`
	CleanupDecision string             `json:"cleanup_decision"`
	CleanupReason   string             `json:"cleanup_reason"`
	ErrorMessage    string             `json:"error_message,omitempty"`
}

func New(mode string) Attempt {
	return Attempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Mode:        mode,
	}
}

func Write(path string, attempt Attempt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir audit dir: %w", err)
	}
	data, err := json.MarshalIndent(attempt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal audit: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}
