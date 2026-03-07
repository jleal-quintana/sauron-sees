package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"sauron-sees/internal/capture"
	"sauron-sees/internal/dailysummary"
	"sauron-sees/internal/state"
)

type logger struct {
	file   *os.File
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
	ds := r.state.EnsureDay(today)

	if r.planner.ShouldAutoClose(now, today, ds.Status != state.StatusOpen, ds.Status == state.StatusClosed) {
		if err := r.closeDay(ctx, today); err != nil {
			return err
		}
	}
	weekKey := r.planner.WeekKey(now)
	if ws := r.state.EnsureWeek(weekKey); r.planner.ShouldAutoCloseWeek(now, weekKey, ws.Status == state.StatusClosed) {
		if err := r.closeWeek(ctx, weekKey); err != nil {
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
		if err := r.closeDay(ctx, day); err != nil {
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
		if err := r.closeWeek(ctx, weekKey); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtimeEnv) closeDay(ctx context.Context, day string) error {
	if ds := r.state.EnsureDay(day); ds.Status == state.StatusClosed {
		r.logger.Printf("day %s already closed", day)
		return nil
	}
	r.state.Seal(day)
	r.state.MarkClosingAttempt(day, time.Now())
	if err := r.saveState(); err != nil {
		return err
	}

	result, err := r.summaries.FinalizeDay(ctx, day)
	if err != nil {
		r.state.MarkFailed(day, err)
		_ = r.saveState()
		return fmt.Errorf("finalize day %s: %w", day, err)
	}
	r.state.MarkClosed(day, result.SummaryPath)
	if err := r.saveState(); err != nil {
		return err
	}
	r.logger.Printf("closed day %s into %s", day, result.SummaryPath)
	weekKey, err := r.planner.WeekKeyForDate(day)
	if err == nil {
		if ws := r.state.EnsureWeek(weekKey); r.planner.ShouldAutoCloseWeek(time.Now(), weekKey, ws.Status == state.StatusClosed) {
			if err := r.closeWeek(ctx, weekKey); err != nil {
				r.logger.Printf("weekly close after day close failed for %s: %v", weekKey, err)
			}
		}
	}
	return nil
}

func (r *runtimeEnv) closeWeek(ctx context.Context, weekKey string) error {
	ws := r.state.EnsureWeek(weekKey)
	if ws.Status == state.StatusClosed {
		r.logger.Printf("week %s already closed", weekKey)
		return nil
	}
	start, end, err := r.planner.WeekRange(weekKey)
	if err != nil {
		return err
	}
	r.state.MarkWeekClosingAttempt(weekKey, time.Now())
	if err := r.saveState(); err != nil {
		return err
	}
	result, err := r.weekly.FinalizeWeek(ctx, weekKey, start, end)
	if err != nil {
		r.state.MarkWeekFailed(weekKey, err)
		_ = r.saveState()
		return fmt.Errorf("finalize week %s: %w", weekKey, err)
	}
	r.state.MarkWeekClosed(weekKey, result.SummaryPath)
	if err := r.saveState(); err != nil {
		return err
	}
	r.logger.Printf("closed week %s into %s", weekKey, result.SummaryPath)
	return nil
}

func (r *runtimeEnv) closeWeekRange(ctx context.Context, from string, to string) error {
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
	r.state.MarkWeekClosingAttempt(weekKey, time.Now())
	if err := r.saveState(); err != nil {
		return err
	}
	result, err := r.weekly.FinalizeWeek(ctx, weekKey, start, end)
	if err != nil {
		r.state.MarkWeekFailed(weekKey, err)
		_ = r.saveState()
		return err
	}
	r.state.MarkWeekClosed(weekKey, result.SummaryPath)
	return r.saveState()
}
