package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	Timezone               string   `toml:"timezone"`
	CaptureIntervalMinutes int      `toml:"capture_interval_minutes"`
	Workdays               []string `toml:"workdays"`
	WorkStart              string   `toml:"work_start"`
	WorkEnd                string   `toml:"work_end"`
	CloseDayTime           string   `toml:"close_day_time"`
	TempRoot               string   `toml:"temp_root"`
	DailyMarkdownRoot      string   `toml:"daily_markdown_root"`
	WeeklyMarkdownRoot     string   `toml:"weekly_markdown_root"`
	CodexProfile           string   `toml:"codex_profile"`
	PromptOverridePath     string   `toml:"prompt_override_path"`
	GranolaEnabled         bool     `toml:"granola_enabled"`
	GranolaMCPServerName   string   `toml:"granola_mcp_server_name"`
	WeeklyAutoEnabled      bool     `toml:"weekly_auto_enabled"`
	WeeklyCloseDay         string   `toml:"weekly_close_day"`
	WeeklyCloseTime        string   `toml:"weekly_close_time"`
	JPEGQuality            int      `toml:"jpeg_quality"`
	ImageMaxDimension      int      `toml:"image_max_dimension"`
	DeleteAfterSuccess     bool     `toml:"delete_after_success"`
	DailySummaryMinWords   int      `toml:"daily_summary_min_words"`
	WeeklySummaryMinWords  int      `toml:"weekly_summary_min_words"`
}

var percentEnvPattern = regexp.MustCompile(`%([A-Za-z0-9_]+)%`)

func Default() Config {
	return Config{
		Timezone:               "America/Argentina/Buenos_Aires",
		CaptureIntervalMinutes: 5,
		Workdays:               []string{"mon", "tue", "wed", "thu", "fri"},
		WorkStart:              "09:00",
		WorkEnd:                "18:30",
		CloseDayTime:           "18:30",
		TempRoot:               `%LOCALAPPDATA%\SauronSees`,
		DailyMarkdownRoot:      `%USERPROFILE%\Documents\Obsidian\Work\Daily`,
		WeeklyMarkdownRoot:     `%USERPROFILE%\Documents\Obsidian\Work\Weekly`,
		CodexProfile:           "sauron-sees-eod",
		PromptOverridePath:     "",
		GranolaEnabled:         true,
		GranolaMCPServerName:   "granola",
		WeeklyAutoEnabled:      true,
		WeeklyCloseDay:         "fri",
		WeeklyCloseTime:        "18:45",
		JPEGQuality:            70,
		ImageMaxDimension:      1600,
		DeleteAfterSuccess:     true,
		DailySummaryMinWords:   120,
		WeeklySummaryMinWords:  180,
	}
}

func Load(explicitPath string) (Config, string, error) {
	cfg := Default()

	path, err := ResolvePath(explicitPath)
	if err != nil {
		return Config{}, "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.expandPaths()
			if err := cfg.Validate(); err != nil {
				return Config{}, "", err
			}
			return cfg, path, nil
		}
		return Config{}, "", fmt.Errorf("read config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, "", fmt.Errorf("parse config: %w", err)
	}
	cfg.expandPaths()
	if err := cfg.Validate(); err != nil {
		return Config{}, "", err
	}
	return cfg, path, nil
}

func ResolvePath(explicitPath string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(base, "SauronSees", "config.toml"), nil
}

func (c *Config) Validate() error {
	if c.Timezone == "" {
		return errors.New("config timezone must not be empty")
	}
	if c.CaptureIntervalMinutes <= 0 {
		return errors.New("config capture_interval_minutes must be > 0")
	}
	if c.TempRoot == "" || c.DailyMarkdownRoot == "" || c.WeeklyMarkdownRoot == "" {
		return errors.New("config temp_root, daily_markdown_root, and weekly_markdown_root must not be empty")
	}
	if _, _, err := ParseClock(c.WorkStart); err != nil {
		return fmt.Errorf("config work_start: %w", err)
	}
	if _, _, err := ParseClock(c.WorkEnd); err != nil {
		return fmt.Errorf("config work_end: %w", err)
	}
	if _, _, err := ParseClock(c.CloseDayTime); err != nil {
		return fmt.Errorf("config close_day_time: %w", err)
	}
	if _, _, err := ParseClock(c.WeeklyCloseTime); err != nil {
		return fmt.Errorf("config weekly_close_time: %w", err)
	}
	if c.JPEGQuality < 1 || c.JPEGQuality > 100 {
		return errors.New("config jpeg_quality must be within 1..100")
	}
	if c.ImageMaxDimension < 320 {
		return errors.New("config image_max_dimension must be >= 320")
	}
	if c.CodexProfile == "" {
		return errors.New("config codex_profile must not be empty")
	}
	if c.DailySummaryMinWords < 50 {
		return errors.New("config daily_summary_min_words must be >= 50")
	}
	if c.WeeklySummaryMinWords < 50 {
		return errors.New("config weekly_summary_min_words must be >= 50")
	}
	return nil
}

func ParseClock(value string) (hour int, minute int, err error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid clock value %q", value)
	}
	var h, m int
	_, err = fmt.Sscanf(value, "%d:%d", &h, &m)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid clock value %q", value)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid clock value %q", value)
	}
	return h, m, nil
}

func ExpandPath(path string) string {
	expanded := percentEnvPattern.ReplaceAllStringFunc(path, func(token string) string {
		name := strings.Trim(token, "%")
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		return token
	})
	return os.ExpandEnv(expanded)
}

func (c *Config) expandPaths() {
	c.TempRoot = ExpandPath(c.TempRoot)
	c.DailyMarkdownRoot = ExpandPath(c.DailyMarkdownRoot)
	c.WeeklyMarkdownRoot = ExpandPath(c.WeeklyMarkdownRoot)
	c.PromptOverridePath = ExpandPath(c.PromptOverridePath)
}
