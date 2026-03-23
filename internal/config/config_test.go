package config

import "testing"

func TestParseConfig(t *testing.T) {
	cfg := Default()
	data := []byte(`
timezone = "America/New_York"
capture_interval_minutes = 10
workdays = ["mon", "wed"]
tray_enabled = false
daily_summary_min_words = 150

[logging]
max_size_mb = 9
max_backups = 3
max_age_days = 21

[work_classification]
advisory_hints_enabled = false
exclude_domains = ["example.com", "example.org"]
`)

	if err := parseConfig(data, &cfg); err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Timezone != "America/New_York" {
		t.Fatalf("Timezone = %q, want %q", cfg.Timezone, "America/New_York")
	}
	if cfg.CaptureIntervalMinutes != 10 {
		t.Fatalf("CaptureIntervalMinutes = %d, want 10", cfg.CaptureIntervalMinutes)
	}
	if len(cfg.Workdays) != 2 || cfg.Workdays[1] != "wed" {
		t.Fatalf("Workdays = %#v", cfg.Workdays)
	}
	if cfg.TrayEnabled {
		t.Fatalf("TrayEnabled = true, want false")
	}
	if cfg.Logging.MaxSizeMB != 9 || cfg.Logging.MaxBackups != 3 || cfg.Logging.MaxAgeDays != 21 {
		t.Fatalf("Logging = %#v", cfg.Logging)
	}
	if cfg.WorkClassification.AdvisoryHintsEnabled {
		t.Fatalf("AdvisoryHintsEnabled = true, want false")
	}
	if len(cfg.WorkClassification.ExcludeDomains) != 2 || cfg.WorkClassification.ExcludeDomains[0] != "example.com" {
		t.Fatalf("ExcludeDomains = %#v", cfg.WorkClassification.ExcludeDomains)
	}
}
