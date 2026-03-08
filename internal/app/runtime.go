package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"sauron-sees/internal/capture"
	"sauron-sees/internal/config"
	"sauron-sees/internal/dailysummary"
	"sauron-sees/internal/state"
	"sauron-sees/internal/tray"
	"sauron-sees/internal/weeklysummary"

	"gopkg.in/natefinch/lumberjack.v2"
)

type logger struct {
	file   io.WriteCloser
	stdout io.Writer
}

func (l *logger) Printf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.New(io.MultiWriter(l.file, l.stdout), "", log.LstdFlags).Println(message)
}

func (l *logger) Close() error {
	return l.file.Close()
}

func (r *runtimeEnv) runAgent(ctx context.Context) error {
	results := dailysummary.Doctor(r.cfg, r.runner)
	if dailysummary.HasBlockingIssue(results) {
		for _, result := range results {
			r.logger.Printf("%s: %s", result.Name, result.Message)
		}
		return fmt.Errorf("doctor found blocking issues")
	}

	if r.trayEnabled {
		go func() {
			if err := tray.Start(ctx, r.trayOptions(ctx)); err != nil {
				r.logger.Printf("tray error: %v", err)
			}
		}()
	}

	now := time.Now()
	if err := r.closePendingDays(ctx, r.currentDay(now)); err != nil {
		r.logger.Printf("pending close error: %v", err)
	}
	if err := r.closePendingWeeks(ctx, r.planner.WeekKey(now)); err != nil {
		r.logger.Printf("pending weekly close error: %v", err)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := r.tick(ctx, time.Now()); err != nil {
			r.logger.Printf("agent tick error: %v", err)
		}
		select {
		case <-ctx.Done():
			r.logger.Printf("agent stopping")
			return nil
		case <-ticker.C:
		}
	}
}

func (r *runtimeEnv) tick(ctx context.Context, now time.Time) error {
	today := r.currentDay(now)
	if err := r.closePendingDays(ctx, today); err != nil {
		return err
	}
	if err := r.closePendingWeeks(ctx, r.planner.WeekKey(now)); err != nil {
		return err
	}
	if r.state.Paused(now) {
		return nil
	}
	ds := r.state.EnsureDay(today)

	if r.planner.ShouldAutoClose(now, today, ds.Status != state.StatusOpen, ds.Status == state.StatusClosed) {
		if err := r.closeDay(ctx, today, false); err != nil {
			return err
		}
	}
	weekKey := r.planner.WeekKey(now)
	if ws := r.state.EnsureWeek(weekKey); r.planner.ShouldAutoCloseWeek(now, weekKey, ws.Status == state.StatusClosed) {
		if err := r.closeWeek(ctx, weekKey, false); err != nil {
			return err
		}
	}
	if r.planner.ShouldCapture(now, r.state.LastCaptureTime(today), ds.Status != state.StatusOpen) {
		if err := r.captureAt(now, today); err != nil {
			if err == capture.ErrSessionLocked {
				r.logger.Printf("capture skipped, session locked")
				return nil
			}
			return err
		}
	}
	return nil
}

func (r *runtimeEnv) captureNow(now time.Time) error {
	day := r.currentDay(now)
	return r.captureAt(now, day)
}

func (r *runtimeEnv) captureAt(now time.Time, day string) error {
	ds := r.state.EnsureDay(day)
	if ds.Status != state.StatusOpen {
		return fmt.Errorf("day %s is sealed or closed; cannot capture", day)
	}
	record, err := r.capturer.Capture(now, day)
	if err != nil {
		return err
	}
	r.state.MarkCapture(day, now)
	if err := r.saveState(); err != nil {
		return err
	}
	r.logger.Printf("captured %s for %s (%s)", record.ImagePath, day, record.ActiveProcess)
	return nil
}

func (r *runtimeEnv) closePendingDays(ctx context.Context, today string) error {
	for _, day := range r.state.PendingBefore(today) {
		if !r.state.ShouldRetry(day) {
			continue
		}
		if err := r.closeDay(ctx, day, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtimeEnv) closePendingWeeks(ctx context.Context, currentWeek string) error {
	for _, weekKey := range r.state.PendingWeeksBefore(currentWeek) {
		if !r.state.ShouldRetryWeek(weekKey) {
			continue
		}
		if err := r.closeWeek(ctx, weekKey, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtimeEnv) closeDay(ctx context.Context, day string, dryRun bool) error {
	if ds := r.state.EnsureDay(day); ds.Status == state.StatusClosed && !dryRun {
		r.logger.Printf("day %s already closed", day)
		return nil
	}
	if !dryRun {
		r.state.Seal(day)
		r.state.MarkClosingAttempt(day, time.Now())
		if err := r.saveState(); err != nil {
			return err
		}
	}

	result, err := r.summaries.FinalizeDay(ctx, day, dailysummary.FinalizeOptions{DryRun: dryRun})
	record := state.AttemptRecord{
		AttemptAt:        time.Now().UTC().Format(time.RFC3339),
		Mode:             ternary(dryRun, "dry-run", "normal"),
		GeneratedPath:    result.SummaryPath,
		VerificationPath: result.VerificationPath,
		VerifierResult:   strings.TrimSpace(result.Verification),
		CleanupDecision:  ternary(result.CleanupEligible, "allow", "deny"),
		CleanupReason:    ternary(result.CleanupEligible, "all checks passed", "validation failed"),
	}
	if err != nil {
		record.ErrorMessage = err.Error()
		r.state.MarkDayAttempt(day, record, dryRun)
		if !dryRun {
			r.state.MarkFailed(day, err)
		}
		_ = r.saveState()
		return fmt.Errorf("finalize day %s: %w", day, err)
	}
	r.state.MarkDayAttempt(day, record, dryRun)
	if dryRun {
		if err := r.saveState(); err != nil {
			return err
		}
		r.logger.Printf("dry-run day %s wrote preview to %s", day, result.SummaryPath)
		return nil
	}
	r.state.MarkClosed(day, result.SummaryPath)
	if err := r.saveState(); err != nil {
		return err
	}
	r.logger.Printf("closed day %s into %s", day, result.SummaryPath)
	weekKey, err := r.planner.WeekKeyForDate(day)
	if err == nil {
		if ws := r.state.EnsureWeek(weekKey); r.planner.ShouldAutoCloseWeek(time.Now(), weekKey, ws.Status == state.StatusClosed) {
			if err := r.closeWeek(ctx, weekKey, false); err != nil {
				r.logger.Printf("weekly close after day close failed for %s: %v", weekKey, err)
			}
		}
	}
	return nil
}

func (r *runtimeEnv) closeWeek(ctx context.Context, weekKey string, dryRun bool) error {
	ws := r.state.EnsureWeek(weekKey)
	if ws.Status == state.StatusClosed && !dryRun {
		r.logger.Printf("week %s already closed", weekKey)
		return nil
	}
	start, end, err := r.planner.WeekRange(weekKey)
	if err != nil {
		return err
	}
	if !dryRun {
		r.state.MarkWeekClosingAttempt(weekKey, time.Now())
		if err := r.saveState(); err != nil {
			return err
		}
	}
	result, err := r.weekly.FinalizeWeek(ctx, weekKey, start, end, weeklysummary.FinalizeOptions{DryRun: dryRun})
	record := state.AttemptRecord{
		AttemptAt:        time.Now().UTC().Format(time.RFC3339),
		Mode:             ternary(dryRun, "dry-run", "normal"),
		GeneratedPath:    result.SummaryPath,
		VerificationPath: result.VerificationPath,
		VerifierResult:   strings.TrimSpace(result.Verification),
		CleanupDecision:  ternary(result.CleanupEligible, "allow", "deny"),
		CleanupReason:    ternary(result.CleanupEligible, "all checks passed", "validation failed"),
	}
	if err != nil {
		record.ErrorMessage = err.Error()
		r.state.MarkWeekAttempt(weekKey, record, dryRun)
		if !dryRun {
			r.state.MarkWeekFailed(weekKey, err)
		}
		_ = r.saveState()
		return fmt.Errorf("finalize week %s: %w", weekKey, err)
	}
	r.state.MarkWeekAttempt(weekKey, record, dryRun)
	if dryRun {
		if err := r.saveState(); err != nil {
			return err
		}
		r.logger.Printf("dry-run week %s wrote preview to %s", weekKey, result.SummaryPath)
		return nil
	}
	r.state.MarkWeekClosed(weekKey, result.SummaryPath)
	if err := r.saveState(); err != nil {
		return err
	}
	r.logger.Printf("closed week %s into %s", weekKey, result.SummaryPath)
	return nil
}

func (r *runtimeEnv) closeWeekRange(ctx context.Context, from string, to string, dryRun bool) error {
	start, err := r.planner.ParseDate(from)
	if err != nil {
		return err
	}
	end, err := r.planner.ParseDate(to)
	if err != nil {
		return err
	}
	if end.Before(start) {
		return fmt.Errorf("--to must be after or equal to --from")
	}
	weekKey := fmt.Sprintf("%s_to_%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	if !dryRun {
		r.state.MarkWeekClosingAttempt(weekKey, time.Now())
		if err := r.saveState(); err != nil {
			return err
		}
	}
	result, err := r.weekly.FinalizeWeek(ctx, weekKey, start, end, weeklysummary.FinalizeOptions{DryRun: dryRun})
	record := state.AttemptRecord{
		AttemptAt:        time.Now().UTC().Format(time.RFC3339),
		Mode:             ternary(dryRun, "dry-run", "normal"),
		GeneratedPath:    result.SummaryPath,
		VerificationPath: result.VerificationPath,
		VerifierResult:   strings.TrimSpace(result.Verification),
		CleanupDecision:  ternary(result.CleanupEligible, "allow", "deny"),
		CleanupReason:    ternary(result.CleanupEligible, "all checks passed", "validation failed"),
	}
	if err != nil {
		record.ErrorMessage = err.Error()
		r.state.MarkWeekAttempt(weekKey, record, dryRun)
		if !dryRun {
			r.state.MarkWeekFailed(weekKey, err)
		}
		_ = r.saveState()
		return err
	}
	r.state.MarkWeekAttempt(weekKey, record, dryRun)
	if !dryRun {
		r.state.MarkWeekClosed(weekKey, result.SummaryPath)
	}
	return r.saveState()
}

func (r *runtimeEnv) pause(duration time.Duration) error {
	until := time.Now().Add(duration)
	r.state.SetPausedUntil(until)
	r.logger.Printf("paused until %s", until.Format(time.RFC3339))
	return r.saveState()
}

func (r *runtimeEnv) resume() error {
	r.state.ClearPause()
	r.logger.Printf("resumed")
	return r.saveState()
}

func (r *runtimeEnv) statusString(now time.Time) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Current day: %s\n", r.currentDay(now))
	fmt.Fprintf(&builder, "Current ISO week: %s\n", r.planner.WeekKey(now))
	if r.state.Paused(now) {
		fmt.Fprintf(&builder, "Paused: yes until %s\n", r.state.PausedUntilTime().Local().Format(time.RFC3339))
	} else {
		fmt.Fprintf(&builder, "Paused: no\n")
	}
	if next := r.planner.NextCaptureAfter(now); !next.IsZero() {
		fmt.Fprintf(&builder, "Next scheduled capture: %s\n", next.Format(time.RFC3339))
	}
	if next := r.planner.NextDailyCloseAfter(now); !next.IsZero() {
		fmt.Fprintf(&builder, "Next scheduled daily close: %s\n", next.Format(time.RFC3339))
	}
	if next := r.planner.NextWeeklyCloseAfter(now); !next.IsZero() {
		fmt.Fprintf(&builder, "Next scheduled weekly close: %s\n", next.Format(time.RFC3339))
	}
	fmt.Fprintf(&builder, "Last successful daily summary: %s\n", blankFallback(r.state.LastSuccessfulClose, "none"))
	lastWeekly := "none"
	for key, ws := range r.state.Weeks {
		if ws.Status == state.StatusClosed && (lastWeekly == "none" || key > lastWeekly) {
			lastWeekly = key
		}
	}
	fmt.Fprintf(&builder, "Last successful weekly summary: %s\n", lastWeekly)
	var pending []string
	for day, ds := range r.state.Days {
		if ds.Status == state.StatusFailed {
			pending = append(pending, "day "+day)
		}
	}
	for week, ws := range r.state.Weeks {
		if ws.Status == state.StatusFailed {
			pending = append(pending, "week "+week)
		}
	}
	if len(pending) == 0 {
		fmt.Fprintf(&builder, "Pending failures: none\n")
	} else {
		fmt.Fprintf(&builder, "Pending failures: %s\n", strings.Join(pending, ", "))
	}
	return builder.String()
}

func blankFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func ternary(condition bool, yes string, no string) string {
	if condition {
		return yes
	}
	return no
}

func openFolder(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func openLogSink(path string, loggingCfg config.LoggingConfig) (io.WriteCloser, error) {
	return &lumberjack.Logger{
		Filename:   path,
		MaxSize:    loggingCfg.MaxSizeMB,
		MaxBackups: loggingCfg.MaxBackups,
		MaxAge:     loggingCfg.MaxAgeDays,
		LocalTime:  true,
	}, nil
}
