package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Timezone               string                   `toml:"timezone"`
	CaptureIntervalMinutes int                      `toml:"capture_interval_minutes"`
	Workdays               []string                 `toml:"workdays"`
	WorkStart              string                   `toml:"work_start"`
	WorkEnd                string                   `toml:"work_end"`
	CloseDayTime           string                   `toml:"close_day_time"`
	TempRoot               string                   `toml:"temp_root"`
	DailyMarkdownRoot      string                   `toml:"daily_markdown_root"`
	WeeklyMarkdownRoot     string                   `toml:"weekly_markdown_root"`
	CodexProfile           string                   `toml:"codex_profile"`
	PromptOverridePath     string                   `toml:"prompt_override_path"`
	GranolaEnabled         bool                     `toml:"granola_enabled"`
	GranolaMCPServerName   string                   `toml:"granola_mcp_server_name"`
	TrayEnabled            bool                     `toml:"tray_enabled"`
	WeeklyAutoEnabled      bool                     `toml:"weekly_auto_enabled"`
	WeeklyCloseDay         string                   `toml:"weekly_close_day"`
	WeeklyCloseTime        string                   `toml:"weekly_close_time"`
	JPEGQuality            int                      `toml:"jpeg_quality"`
	ImageMaxDimension      int                      `toml:"image_max_dimension"`
	DeleteAfterSuccess     bool                     `toml:"delete_after_success"`
	DailySummaryMinWords   int                      `toml:"daily_summary_min_words"`
	WeeklySummaryMinWords  int                      `toml:"weekly_summary_min_words"`
	Logging                LoggingConfig            `toml:"logging"`
	WorkClassification     WorkClassificationConfig `toml:"work_classification"`
}

type LoggingConfig struct {
	MaxSizeMB  int `toml:"max_size_mb"`
	MaxBackups int `toml:"max_backups"`
	MaxAgeDays int `toml:"max_age_days"`
}

type WorkClassificationConfig struct {
	AdvisoryHintsEnabled bool     `toml:"advisory_hints_enabled"`
	IncludeApps          []string `toml:"include_apps"`
	ExcludeApps          []string `toml:"exclude_apps"`
	IncludeTitles        []string `toml:"include_titles"`
	ExcludeTitles        []string `toml:"exclude_titles"`
	IncludeDomains       []string `toml:"include_domains"`
	ExcludeDomains       []string `toml:"exclude_domains"`
	Notes                []string `toml:"notes"`
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
		TrayEnabled:            true,
		WeeklyAutoEnabled:      true,
		WeeklyCloseDay:         "fri",
		WeeklyCloseTime:        "18:45",
		JPEGQuality:            70,
		ImageMaxDimension:      1600,
		DeleteAfterSuccess:     true,
		DailySummaryMinWords:   120,
		WeeklySummaryMinWords:  180,
		Logging: LoggingConfig{
			MaxSizeMB:  5,
			MaxBackups: 5,
			MaxAgeDays: 14,
		},
		WorkClassification: WorkClassificationConfig{
			AdvisoryHintsEnabled: true,
			ExcludeDomains:       []string{"twitter.com", "x.com", "reddit.com", "youtube.com"},
		},
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

	if err := parseConfig(data, &cfg); err != nil {
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
	if c.Logging.MaxSizeMB <= 0 {
		return errors.New("config logging.max_size_mb must be > 0")
	}
	if c.Logging.MaxBackups < 1 {
		return errors.New("config logging.max_backups must be >= 1")
	}
	if c.Logging.MaxAgeDays < 1 {
		return errors.New("config logging.max_age_days must be >= 1")
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

func parseConfig(data []byte, cfg *Config) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	section := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = stripInlineComment(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid line %q", line)
		}
		fullKey := strings.TrimSpace(key)
		if section != "" {
			fullKey = section + "." + fullKey
		}
		if err := assignConfigValue(cfg, fullKey, strings.TrimSpace(value)); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func assignConfigValue(cfg *Config, key string, raw string) error {
	switch key {
	case "timezone":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.Timezone = value
	case "capture_interval_minutes":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.CaptureIntervalMinutes = value
	case "workdays":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.Workdays = value
	case "work_start":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkStart = value
	case "work_end":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkEnd = value
	case "close_day_time":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.CloseDayTime = value
	case "temp_root":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.TempRoot = value
	case "daily_markdown_root":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.DailyMarkdownRoot = value
	case "weekly_markdown_root":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.WeeklyMarkdownRoot = value
	case "codex_profile":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.CodexProfile = value
	case "prompt_override_path":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.PromptOverridePath = value
	case "granola_enabled":
		value, err := parseBoolValue(raw)
		if err != nil {
			return err
		}
		cfg.GranolaEnabled = value
	case "granola_mcp_server_name":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.GranolaMCPServerName = value
	case "tray_enabled":
		value, err := parseBoolValue(raw)
		if err != nil {
			return err
		}
		cfg.TrayEnabled = value
	case "weekly_auto_enabled":
		value, err := parseBoolValue(raw)
		if err != nil {
			return err
		}
		cfg.WeeklyAutoEnabled = value
	case "weekly_close_day":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.WeeklyCloseDay = value
	case "weekly_close_time":
		value, err := parseStringValue(raw)
		if err != nil {
			return err
		}
		cfg.WeeklyCloseTime = value
	case "jpeg_quality":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.JPEGQuality = value
	case "image_max_dimension":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.ImageMaxDimension = value
	case "delete_after_success":
		value, err := parseBoolValue(raw)
		if err != nil {
			return err
		}
		cfg.DeleteAfterSuccess = value
	case "daily_summary_min_words":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.DailySummaryMinWords = value
	case "weekly_summary_min_words":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.WeeklySummaryMinWords = value
	case "logging.max_size_mb":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.Logging.MaxSizeMB = value
	case "logging.max_backups":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.Logging.MaxBackups = value
	case "logging.max_age_days":
		value, err := parseIntValue(raw)
		if err != nil {
			return err
		}
		cfg.Logging.MaxAgeDays = value
	case "work_classification.advisory_hints_enabled":
		value, err := parseBoolValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.AdvisoryHintsEnabled = value
	case "work_classification.include_apps":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.IncludeApps = value
	case "work_classification.exclude_apps":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.ExcludeApps = value
	case "work_classification.include_titles":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.IncludeTitles = value
	case "work_classification.exclude_titles":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.ExcludeTitles = value
	case "work_classification.include_domains":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.IncludeDomains = value
	case "work_classification.exclude_domains":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.ExcludeDomains = value
	case "work_classification.notes":
		value, err := parseStringArrayValue(raw)
		if err != nil {
			return err
		}
		cfg.WorkClassification.Notes = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func stripInlineComment(line string) string {
	inString := false
	for i := 0; i < len(line); i++ {
		if line[i] == '"' && (i == 0 || line[i-1] != '\\') {
			inString = !inString
		}
		if !inString && line[i] == '#' {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func parseStringValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("invalid string value %q", raw)
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("invalid string value %q", raw)
	}
	return unquoted, nil
}

func parseIntValue(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("invalid int value %q", raw)
	}
	return value, nil
}

func parseBoolValue(raw string) (bool, error) {
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("invalid bool value %q", raw)
	}
	return value, nil
}

func parseStringArrayValue(raw string) ([]string, error) {
	value := strings.TrimSpace(raw)
	if value == "[]" {
		return []string{}, nil
	}
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("invalid array value %q", raw)
	}
	value = strings.TrimSpace(value[1 : len(value)-1])
	if value == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item, err := parseStringValue(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
