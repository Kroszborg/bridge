// Package schedule sends messages at set times: once, or repeating daily,
// weekly or monthly, at a wall-clock time in an IANA time zone.
package schedule

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // schedules name IANA zones; minimal images have no zoneinfo
)

// Kinds of schedule.
const (
	KindOnce    = "once"
	KindDaily   = "daily"
	KindWeekly  = "weekly"
	KindMonthly = "monthly"
)

// Weekdays, as used in Spec.Days.
var Weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Spec is when a schedule runs.
type Spec struct {
	Kind       string
	At         string   // "HH:MM", 24-hour, in TimeZone
	Days       []string // weekly: weekdays, "mon".."sun"
	DayOfMonth int      // monthly: 1..31, clamped to the month's last day
	Date       string   // once: "YYYY-MM-DD"
	TimeZone   string   // IANA, e.g. "Asia/Kolkata"
}

// SpecError explains why a spec is invalid.
type SpecError struct{ Field, Message string }

func (e *SpecError) Error() string { return e.Field + ": " + e.Message }

// Normalize validates a spec, fills derived fields and returns its location.
// Fields that do not apply to the kind are cleared.
func (s *Spec) Normalize() (*time.Location, error) {
	switch s.Kind {
	case KindOnce, KindDaily, KindWeekly, KindMonthly:
	default:
		return nil, &SpecError{"kind", "Use once, daily, weekly or monthly."}
	}
	if _, _, ok := parseClock(s.At); !ok {
		return nil, &SpecError{"at", "Use 24-hour HH:MM, for example 09:30."}
	}
	tz := strings.TrimSpace(s.TimeZone)
	if tz == "" || tz == "Local" {
		return nil, &SpecError{"time_zone", "Name an IANA time zone, for example Asia/Kolkata or America/New_York."}
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, &SpecError{"time_zone", "Unknown time zone " + strconv.Quote(tz) + ". Use an IANA name such as Europe/London."}
	}
	s.TimeZone = tz

	if s.Kind != KindWeekly {
		s.Days = nil
	}
	if s.Kind != KindMonthly {
		s.DayOfMonth = 0
	}
	if s.Kind != KindOnce {
		s.Date = ""
	}
	switch s.Kind {
	case KindOnce:
		if _, err := time.Parse(time.DateOnly, s.Date); err != nil {
			return nil, &SpecError{"date", "A one-off schedule needs a date as YYYY-MM-DD."}
		}
	case KindWeekly:
		days := make([]string, 0, len(s.Days))
		for _, d := range s.Days {
			d = strings.ToLower(strings.TrimSpace(d))
			if len(d) > 3 {
				d = d[:3] // "monday" → "mon"
			}
			if !slices.Contains(Weekdays, d) {
				return nil, &SpecError{"days", "Use weekday names: mon, tue, wed, thu, fri, sat, sun."}
			}
			if !slices.Contains(days, d) {
				days = append(days, d)
			}
		}
		if len(days) == 0 {
			return nil, &SpecError{"days", "A weekly schedule needs at least one weekday."}
		}
		slices.SortFunc(days, func(a, b string) int { return weekdayIndex(a) - weekdayIndex(b) })
		s.Days = days
	case KindMonthly:
		if s.DayOfMonth < 1 || s.DayOfMonth > 31 {
			return nil, &SpecError{"day_of_month", "Use a day from 1 to 31; days past a month's end run on its last day."}
		}
	}
	return loc, nil
}

func weekdayIndex(d string) int { return slices.Index(Weekdays, d) }

func parseClock(s string) (h, m int, ok bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// Next returns the first run strictly after `after`, or ok=false when the
// schedule has no further runs (a one-off whose time has passed).
//
// Times are wall-clock times in the spec's zone, so a 09:00 daily schedule
// stays at 09:00 across daylight-saving changes. A time that does not exist
// on a given day (skipped when clocks go forward) runs at the same distance
// past the gap, e.g. 02:30 becomes 03:30; a time that occurs twice (when
// clocks go back) runs once, at its first occurrence.
func (s Spec) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	h, m, ok := parseClock(s.At)
	if !ok {
		return time.Time{}, false
	}
	if s.Kind == KindOnce {
		d, err := time.Parse(time.DateOnly, s.Date)
		if err != nil {
			return time.Time{}, false
		}
		t := wallTime(d.Year(), d.Month(), d.Day(), h, m, loc)
		return t, t.After(after)
	}
	local := after.In(loc)
	// Start a day early: a run late on the previous local day can still be
	// after `after` when the gap rule moved it forward.
	day := time.Date(local.Year(), local.Month(), local.Day()-1, 12, 0, 0, 0, time.UTC)
	for range 400 {
		if s.runsOn(day) {
			if t := wallTime(day.Year(), day.Month(), day.Day(), h, m, loc); t.After(after) {
				return t, true
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// runsOn reports whether the schedule runs on a calendar date (given at noon UTC).
func (s Spec) runsOn(day time.Time) bool {
	switch s.Kind {
	case KindDaily:
		return true
	case KindWeekly:
		return slices.Contains(s.Days, Weekdays[day.Weekday()])
	case KindMonthly:
		last := time.Date(day.Year(), day.Month()+1, 0, 12, 0, 0, 0, time.UTC).Day()
		return day.Day() == min(s.DayOfMonth, last)
	}
	return false
}

// wallTime is the instant a wall clock in loc shows the given date and time.
// See Next for how gaps and repeated times resolve.
func wallTime(y int, mo time.Month, d, h, mi int, loc *time.Location) time.Time {
	wall := time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
	_, before := wall.Add(-36 * time.Hour).In(loc).Zone()
	_, after := wall.Add(36 * time.Hour).In(loc).Zone()
	var best time.Time
	for _, off := range []int{before, after} {
		t := wall.Add(-time.Duration(off) * time.Second)
		lt := t.In(loc)
		if lt.Year() == y && lt.Month() == mo && lt.Day() == d && lt.Hour() == h && lt.Minute() == mi {
			if best.IsZero() || t.Before(best) {
				best = t
			}
		}
	}
	if !best.IsZero() {
		return best
	}
	// In a gap: read the time with the offset in force before the change,
	// which lands as far past the gap as the time was into it.
	return wall.Add(-time.Duration(before) * time.Second)
}

// Describe renders a spec in words, e.g. "Every Mon, Wed at 09:00 (Asia/Kolkata)".
func (s Spec) Describe() string {
	var when string
	switch s.Kind {
	case KindOnce:
		when = "Once on " + s.Date
	case KindDaily:
		when = "Every day"
	case KindWeekly:
		names := make([]string, 0, len(s.Days))
		for _, d := range s.Days {
			names = append(names, strings.ToUpper(d[:1])+d[1:])
		}
		when = "Every " + strings.Join(names, ", ")
	case KindMonthly:
		when = fmt.Sprintf("Monthly on day %d", s.DayOfMonth)
	}
	return fmt.Sprintf("%s at %s (%s)", when, s.At, s.TimeZone)
}
