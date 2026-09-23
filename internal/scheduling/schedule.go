// Package scheduling implements business-local schedules shared by both domains.
package scheduling

import (
	"fmt"
	"slices"
	"time"
)

type Hour struct {
	DayOfWeek int    `json:"day_of_week"`
	OpensAt   string `json:"opens_at"`
	ClosesAt  string `json:"closes_at"`
	IsClosed  bool   `json:"is_closed"`
}
type Slot struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

func Clock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("time must be HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}
func Validate(hours []Hour, dates []string) error {
	if len(hours) > 7 || len(dates) > 366 {
		return fmt.Errorf("schedule is too large")
	}
	seen := map[int]bool{}
	for _, h := range hours {
		if h.DayOfWeek < 0 || h.DayOfWeek > 6 || seen[h.DayOfWeek] {
			return fmt.Errorf("invalid or duplicate weekday")
		}
		seen[h.DayOfWeek] = true
		if h.IsClosed {
			continue
		}
		a, e := Clock(h.OpensAt)
		b, f := Clock(h.ClosesAt)
		if e != nil || f != nil || a == b {
			return fmt.Errorf("invalid opening interval")
		}
	}
	for _, d := range dates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return fmt.Errorf("invalid closure date")
		}
	}
	return nil
}

// Iterate real instants, then compare wall-clock minutes. This skips DST gaps and
// preserves both offsets in a repeated hour without inventing nonexistent times.
func Slots(date, zone string, hours []Hour, closed []string, step, duration, buffer int, now time.Time, lead, horizon int) ([]Slot, error) {
	out := []Slot{}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return out, err
	}
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return out, err
	}
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	if day.Before(today) || !day.Before(today.AddDate(0, 0, horizon+1)) || slices.Contains(closed, date) {
		return out, nil
	}
	if step < 1 || duration < 1 {
		return out, fmt.Errorf("invalid slot duration")
	}
	// Consider the previous day's overnight opening and this day's opening.
	for start := day; start.Before(day.AddDate(0, 0, 1)); start = start.Add(time.Minute) {
		if start.Before(now.Add(time.Duration(lead) * time.Minute)) {
			continue
		}
		local := start.In(loc)
		minute := local.Hour()*60 + local.Minute()
		for _, h := range hours {
			if h.IsClosed {
				continue
			}
			a, e := Clock(h.OpensAt)
			b, f := Clock(h.ClosesAt)
			if e != nil || f != nil {
				continue
			}
			weekday := (int(local.Weekday()) + 6) % 7
			offset := minute - a
			endMinute := b
			openingDay := day
			if b < a {
				endMinute += 1440
				if minute < b {
					weekday = (weekday + 6) % 7
					offset = minute + 1440 - a
					openingDay = day.AddDate(0, 0, -1)
				}
			}
			if weekday != h.DayOfWeek || offset < 0 || offset%step != 0 || slices.Contains(closed, openingDay.Format("2006-01-02")) {
				continue
			}
			end := start.Add(time.Duration(duration+buffer) * time.Minute)
			// Verify every occupied instant stays within the opening and no closed date.
			valid := true
			for probe := start; probe.Before(end); probe = probe.Add(time.Minute) {
				pl := probe.In(loc)
				dayOffset := 0
				if pl.Format("2006-01-02") != openingDay.Format("2006-01-02") {
					dayOffset = 1440
				}
				wall := pl.Hour()*60 + pl.Minute() + dayOffset
				if wall < a || wall >= endMinute || slices.Contains(closed, pl.Format("2006-01-02")) {
					valid = false
					break
				}
			}
			if valid {
				out = append(out, Slot{start, start.Add(time.Duration(duration) * time.Minute)})
				break
			}
		}
	}
	return out, nil
}
