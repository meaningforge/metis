package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestPlanCustomCalendarCumulative(t *testing.T) {
	baseTime := &ossie.Field{Name: "order_date"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	ordinal := &ossie.Field{Name: "fiscal_week_ordinal"}
	grain := query.TimeGrain("fiscal_week")
	groups := []GroupBy{{
		Name: "order_date@fiscal_week",
		CustomCalendar: &CustomCalendarGrouping{
			Grain: grain, Dataset: "calendar", DatasetSource: "analytics.calendar",
			BaseTimeField: baseTime, BucketField: bucket, BucketExpression: "calendar.fiscal_week_start",
			OrdinalField: ordinal, OrdinalExpression: "calendar.fiscal_week_ordinal",
		},
	}}
	spec := ossie.CumulativeMetricSpec{
		Kind: ossie.MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date",
		Window: ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"},
	}
	nodes := []SemanticPlanNode{CumulativeWindowNode{Base: SemanticPlanNodeBase{ID: "rolling_revenue", OutputGrain: groups}, Spec: spec}}
	if err := planCustomCalendarCumulatives(nodes); err != nil {
		t.Fatal(err)
	}
	owned := nodes[0].(CumulativeWindowNode).CustomCalendarRolling
	if owned == nil {
		t.Fatal("custom rolling cumulative plan missing")
	}
	planned := *owned
	if planned.Grain != grain || planned.Count != 3 {
		t.Fatalf("plan = %#v", planned)
	}
	if planned.Dataset.Name != "calendar" || planned.Dataset.Source != "analytics.calendar" {
		t.Fatalf("dataset = %#v", planned.Dataset)
	}
	if planned.BucketField != bucket || planned.OrdinalField != ordinal {
		t.Fatalf("calendar fields not preserved: %#v", planned)
	}
	if owned.Count != 3 || owned.Dataset.Name != "calendar" {
		t.Fatalf("stage-owned custom rolling mapping = %#v", owned)
	}
}

func TestPlanCustomCalendarCumulativeRequiresMatchingQueryGrain(t *testing.T) {
	month := query.TimeGrainMonth
	groups := []GroupBy{{Name: "order_date@month", Grain: &month}}
	spec := ossie.CumulativeMetricSpec{Kind: ossie.MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"}}
	if _, err := planCustomCalendarCumulative("rolling_revenue", spec, groups); err == nil {
		t.Fatal("expected matching custom query grain requirement")
	}
}

func TestPlanCustomCalendarCumulativesIgnoreBuiltInWindows(t *testing.T) {
	spec := ossie.CumulativeMetricSpec{Kind: ossie.MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "month"}}
	nodes := []SemanticPlanNode{CumulativeWindowNode{Base: SemanticPlanNodeBase{ID: "rolling_revenue"}, Spec: spec}}
	if err := planCustomCalendarCumulatives(nodes); err != nil {
		t.Fatal(err)
	}
	cumulative := nodes[0].(CumulativeWindowNode)
	if cumulative.CustomCalendarRolling != nil {
		t.Fatal("built-in rolling window unexpectedly owns custom mapping")
	}
}
