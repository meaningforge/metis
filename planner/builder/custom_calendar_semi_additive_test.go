package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestQueriedSemiAdditiveTimeWindowRecognizesCustomCalendar(t *testing.T) {
	base := &ossie.Field{Name: "snapshot_date"}
	bucket := &ossie.Field{Name: "fiscal_week"}
	ordinal := &ossie.Field{Name: "fiscal_week_ordinal"}
	groups := []semanticplan.GroupBy{{
		Name:  "snapshot_date@fiscal_week",
		Field: bucket,
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Grain:         query.TimeGrain("fiscal_week"),
			BaseTimeField: base,
			BucketField:   bucket,
			OrdinalField:  ordinal,
		},
	}}
	if !queriedSemiAdditiveTimeWindow(groups, "snapshot_date") {
		t.Fatal("custom-calendar query grain must define an independent semi-additive selection window")
	}
}

func TestQueriedSemiAdditiveTimeWindowDoesNotMatchDifferentCustomBaseTime(t *testing.T) {
	groups := []semanticplan.GroupBy{{
		Name: "event_date@fiscal_week",
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Grain:         query.TimeGrain("fiscal_week"),
			BaseTimeField: &ossie.Field{Name: "event_date"},
		},
	}}
	if queriedSemiAdditiveTimeWindow(groups, "snapshot_date") {
		t.Fatal("custom-calendar query grain for another base time dimension must not affect semi-additive selection")
	}
}
