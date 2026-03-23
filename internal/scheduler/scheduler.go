package scheduler

import (
	"fmt"
	"strings"
	"time"

	"sauron-sees/internal/config"
)

type Planner struct {
	location          *time.Location
	workdays          map[time.Weekday]bool
	workStartMins     int
	workEndMins       int
	closeMins         int
	weeklyCloseDay    time.Weekday
	weeklyCloseMins   int
	weeklyAutoEnabled bool
	interval          time.Duration
}

func New(cfg config.Config) (*Planner, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}
	startHour, startMin, err := config.ParseClock(cfg.WorkStart)
	if err != nil {
		return nil, err
	}
	endHour, endMin, err := config.ParseClock(cfg.WorkEnd)
	if err != nil {
		return nil, err
	}
	closeHour, closeMin, err := config.ParseClock(cfg.CloseDayTime)
	if err != nil {
		return nil, err
	}
	weeklyHour, weeklyMin, err := config.ParseClock(cfg.WeeklyCloseTime)
	if err != nil {
		return nil, err
	}
	weeklyDay, ok := weekdayToken(cfg.WeeklyCloseDay)
	if !ok {
		return nil, fmt.Errorf("invalid weekly_close_day %q", cfg.WeeklyCloseDay)
	}

	workdays := map[time.Weekday]bool{}
	for _, day := range cfg.Workdays {
		if wd, ok := weekdayToken(day); ok {
			workdays[wd] = true
		}
	}

	return &Planner{
		location:          loc,
		workdays:          workdays,
		workStartMins:     startHour*60 + startMin,
		workEndMins:       endHour*60 + endMin,
		closeMins:         closeHour*60 + closeMin,
		weeklyCloseDay:    weeklyDay,
		weeklyCloseMins:   weeklyHour*60 + weeklyMin,
		weeklyAutoEnabled: cfg.WeeklyAutoEnabled,
		interval:          time.Duration(cfg.CaptureIntervalMinutes) * time.Minute,
	}, nil
}

func (p *Planner) LocalDate(t time.Time) string {
	return t.In(p.location).Format("2006-01-02")
}

func (p *Planner) Location() *time.Location {
	return p.location
}

func (p *Planner) HasRolledOver(a, b time.Time) bool {
	return p.LocalDate(a) != p.LocalDate(b)
}

func (p *Planner) WithinWorkWindow(t time.Time) bool {
	local := t.In(p.location)
	if len(p.workdays) > 0 && !p.workdays[local.Weekday()] {
		return false
	}
	nowMins := local.Hour()*60 + local.Minute()
	if p.workStartMins <= p.workEndMins {
		return nowMins >= p.workStartMins && nowMins <= p.workEndMins
	}
	return nowMins >= p.workStartMins || nowMins <= p.workEndMins
}

func (p *Planner) ShouldCapture(now time.Time, lastCapture time.Time, sealed bool) bool {
	if sealed || !p.WithinWorkWindow(now) {
		return false
	}
	if lastCapture.IsZero() {
		return true
	}
	localNow := now.In(p.location)
	localLast := lastCapture.In(p.location)
	if p.LocalDate(localNow) != p.LocalDate(localLast) {
		return true
	}
	return localNow.Sub(localLast) >= p.interval
}

func (p *Planner) ShouldAutoClose(now time.Time, day string, sealed bool, alreadyClosedToday bool) bool {
	if sealed || alreadyClosedToday || day == "" {
		return false
	}
	local := now.In(p.location)
	if p.LocalDate(local) != day {
		return true
	}
	nowMins := local.Hour()*60 + local.Minute()
	return nowMins >= p.closeMins
}

func (p *Planner) ParseDate(day string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", day, p.location)
}

func (p *Planner) WeekKey(t time.Time) string {
	local := t.In(p.location)
	year, week := local.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func (p *Planner) WeekKeyForDate(day string) (string, error) {
	t, err := p.ParseDate(day)
	if err != nil {
		return "", err
	}
	return p.WeekKey(t), nil
}

func (p *Planner) WeekRange(weekKey string) (time.Time, time.Time, error) {
	var year, week int
	if _, err := fmt.Sscanf(weekKey, "%4d-W%2d", &year, &week); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid week key %q", weekKey)
	}
	start := time.Date(year, 1, 4, 0, 0, 0, 0, p.location)
	for start.Weekday() != time.Monday {
		start = start.AddDate(0, 0, -1)
	}
	start = start.AddDate(0, 0, (week-1)*7)
	end := start.AddDate(0, 0, 6)
	return start, end, nil
}

func (p *Planner) ShouldAutoCloseWeek(now time.Time, weekKey string, alreadyClosed bool) bool {
	if alreadyClosed || !p.weeklyAutoEnabled || weekKey == "" {
		return false
	}
	currentWeek := p.WeekKey(now)
	if weekKey != currentWeek {
		return weekKey < currentWeek
	}
	local := now.In(p.location)
	if local.Weekday() != p.weeklyCloseDay {
		return false
	}
	return local.Hour()*60+local.Minute() >= p.weeklyCloseMins
}

func (p *Planner) NextCaptureAfter(now time.Time) time.Time {
	local := now.In(p.location)
	for dayOffset := 0; dayOffset < 14; dayOffset++ {
		day := local.AddDate(0, 0, dayOffset)
		if len(p.workdays) > 0 && !p.workdays[day.Weekday()] {
			continue
		}
		base := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, p.location)
		candidate := base.Add(time.Duration(p.workStartMins) * time.Minute)
		if dayOffset == 0 && local.After(candidate) {
			elapsed := local.Sub(candidate)
			steps := int(elapsed / p.interval)
			candidate = candidate.Add(time.Duration(steps+1) * p.interval)
		}
		windowEnd := base.Add(time.Duration(p.workEndMins) * time.Minute)
		if candidate.After(windowEnd) {
			continue
		}
		if candidate.After(local) {
			return candidate
		}
	}
	return time.Time{}
}

func (p *Planner) NextDailyCloseAfter(now time.Time) time.Time {
	local := now.In(p.location)
	for dayOffset := 0; dayOffset < 14; dayOffset++ {
		day := local.AddDate(0, 0, dayOffset)
		base := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, p.location)
		candidate := base.Add(time.Duration(p.closeMins) * time.Minute)
		if candidate.After(local) {
			return candidate
		}
	}
	return time.Time{}
}

func (p *Planner) NextWeeklyCloseAfter(now time.Time) time.Time {
	local := now.In(p.location)
	for dayOffset := 0; dayOffset < 21; dayOffset++ {
		day := local.AddDate(0, 0, dayOffset)
		if day.Weekday() != p.weeklyCloseDay {
			continue
		}
		base := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, p.location)
		candidate := base.Add(time.Duration(p.weeklyCloseMins) * time.Minute)
		if candidate.After(local) {
			return candidate
		}
	}
	return time.Time{}
}

func weekdayToken(value string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sun":
		return time.Sunday, true
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}
