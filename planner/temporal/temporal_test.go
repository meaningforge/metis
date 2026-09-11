package temporal

import (
	"testing"

	"github.com/meaningforge/metis/query"
)

func TestShiftPreservesLiteralLayout(t *testing.T) {
	cases := []struct {
		name  string
		value string
		count int
		unit  query.TimeGrain
		want  string
	}{
		{name: "hour", value: "2026-03-10T12:00:00Z", count: -3, unit: query.TimeGrainHour, want: "2026-03-10T09:00:00Z"},
		{name: "day", value: "2026-03-10", count: -2, unit: query.TimeGrainDay, want: "2026-03-08"},
		{name: "week", value: "2026-03-10", count: -2, unit: query.TimeGrainWeek, want: "2026-02-24"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Shift(tc.value, tc.count, tc.unit)
			if err != nil || got != tc.want {
				t.Fatalf("Shift(%q, %d, %q) = %q, %v; want %q", tc.value, tc.count, tc.unit, got, err, tc.want)
			}
		})
	}
}

func TestPeriodStart(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit query.TimeGrain
		want string
	}{
		{name: "quarter", unit: query.TimeGrainQuarter, want: "2026-04-01"},
		{name: "week", unit: query.TimeGrainWeek, want: "2026-05-11"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PeriodStart("2026-05-17", tc.unit)
			if err != nil || got != tc.want {
				t.Fatalf("PeriodStart = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestGrainOrdering(t *testing.T) {
	if !IsFinerGrain(query.TimeGrainMonth, query.TimeGrainYear) {
		t.Fatal("month should be finer than year")
	}
	if !IsCoarserGrain(query.TimeGrainYear, query.TimeGrainMonth) {
		t.Fatal("year should be coarser than month")
	}
}
