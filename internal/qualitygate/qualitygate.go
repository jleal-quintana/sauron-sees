package qualitygate

import (
	"context"
	"fmt"
	"os"
	"strings"

	"sauron-sees/internal/codex"
)

func WordCount(text string) int {
	return len(strings.Fields(text))
}

func VerifyFileAndContent(ctx context.Context, runner codex.Runner, profile string, workingDir string, kind string, path string, content string, minWords int) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s file check failed: %w", kind, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s path is a directory, expected file", kind)
	}
	if words := WordCount(content); words < minWords {
		return fmt.Errorf("%s has %d words, below minimum %d", kind, words, minWords)
	}

	prompt := fmt.Sprintf(`You are validating a generated %s markdown.

Return exactly one word:
- SAFE if the markdown is complete, coherent, non-empty, not truncated, has the expected sections, and is safe to accept as final.
- UNSAFE if it looks incomplete, vague, placeholder-like, malformed, empty, or suspicious.

Do not explain your reasoning. Output exactly SAFE or UNSAFE.

Markdown:
---
%s
---`, kind, strings.TrimSpace(content))

	response, err := runner.Run(ctx, codex.Request{
		WorkingDir: workingDir,
		Profile:    profile,
		Prompt:     prompt,
	})
	if err != nil {
		return fmt.Errorf("%s verifier failed: %w", kind, err)
	}
	if strings.TrimSpace(response) != "SAFE" {
		return fmt.Errorf("%s verifier returned %q", kind, strings.TrimSpace(response))
	}
	return nil
}
