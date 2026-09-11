// Package temporal owns deterministic built-in time-grain ordering and
// timezone-preserving temporal literal operations shared by builder and
// conversion. It owns no planner state, metric semantics, or predicate
// placement.
package temporal

import (
	"fmt"
	"time"

	"github.com/meaningforge/metis/query"
)

// IsFinerGrain reports whether queryGrain is strictly finer than reset.
func IsFinerGrain(queryGrain, reset query.TimeGrain) bool {
	q, qOK := grainRank(queryGrain)
	r, rOK := grainRank(reset)
	return qOK && rOK && q > r
}

// IsCoarserGrain reports whether a is coarser than b, treating an unknown b
// as finer so callers can select a known widest read range.
func IsCoarserGrain(a, b query.TimeGrain) bool {
	aRank, aOK := grainRank(a)
	bRank, bOK := grainRank(b)
	return aOK && (!bOK || aRank < bRank)
}

// Shift moves a supported temporal literal by count units while preserving its
// input layout and timezone.
func Shift(value string, count int, unit query.TimeGrain) (string, error) {
	parsed, layout, err := parse(value)
	if err != nil {
		return "", err
	}
	switch unit {
	case query.TimeGrainHour:
		parsed = parsed.Add(time.Duration(count) * time.Hour)
	case query.TimeGrainDay:
		parsed = parsed.AddDate(0, 0, count)
	case query.TimeGrainWeek:
		parsed = parsed.AddDate(0, 0, count*7)
	case query.TimeGrainMonth:
		parsed = parsed.AddDate(0, count, 0)
	case query.TimeGrainQuarter:
		parsed = parsed.AddDate(0, count*3, 0)
	case query.TimeGrainYear:
		parsed = parsed.AddDate(count, 0, 0)
	default:
		return "", fmt.Errorf("unsupported historical offset unit %q", unit)
	}
	return parsed.Format(layout), nil
}

// PeriodStart returns the beginning of the built-in period containing value.
func PeriodStart(value string, unit query.TimeGrain) (string, error) {
	parsed, layout, err := parse(value)
	if err != nil {
		return "", err
	}
	year, month, day := parsed.Date()
	location := parsed.Location()
	switch unit {
	case query.TimeGrainYear:
		parsed = time.Date(year, time.January, 1, 0, 0, 0, 0, location)
	case query.TimeGrainQuarter:
		month = time.Month(((int(month)-1)/3)*3 + 1)
		parsed = time.Date(year, month, 1, 0, 0, 0, 0, location)
	case query.TimeGrainMonth:
		parsed = time.Date(year, month, 1, 0, 0, 0, 0, location)
	case query.TimeGrainWeek:
		midnight := time.Date(year, month, day, 0, 0, 0, 0, location)
		parsed = midnight.AddDate(0, 0, -(int(midnight.Weekday())+6)%7)
	case query.TimeGrainDay:
		parsed = time.Date(year, month, day, 0, 0, 0, 0, location)
	default:
		return "", fmt.Errorf("unsupported grain-to-date reset unit %q", unit)
	}
	return parsed.Format(layout), nil
}

// HourStart returns the beginning of the hour containing value.
func HourStart(value string) (string, error) {
	parsed, layout, err := parse(value)
	if err != nil {
		return "", err
	}
	parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), 0, 0, 0, parsed.Location())
	return parsed.Format(layout), nil
}

// Parse parses a supported semantic temporal literal and reports its original
// layout so callers can compare values without changing their representation.
func Parse(value string) (time.Time, string, error) {
	return parse(value)
}

func grainRank(grain query.TimeGrain) (int, bool) {
	switch grain {
	case query.TimeGrainYear:
		return 0, true
	case query.TimeGrainQuarter:
		return 1, true
	case query.TimeGrainMonth:
		return 2, true
	case query.TimeGrainWeek:
		return 3, true
	case query.TimeGrainDay:
		return 4, true
	case query.TimeGrainHour:
		return 5, true
	default:
		return 0, false
	}
}

func parse(value string) (time.Time, string, error) {
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, layout, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("parse temporal value %q: expected YYYY-MM-DD or RFC3339-like timestamp", value)
}
