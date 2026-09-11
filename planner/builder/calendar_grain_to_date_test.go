package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestPlanCustomCalendarGrainToDate(t *testing.T) {
	baseTime := &ossie.Field{Name: "order_date"}
	weekBucket := &ossie.Field{Name: "fiscal_week_start"}
	weekOrdinal := &ossie.Field{Name: "fiscal_week_ordinal"}
	quarterBucket := &ossie.Field{Name: "fiscal_quarter_start"}
	quarterOrdinal := &ossie.Field{Name: "fiscal_quarter_ordinal"}
	spec := ossie.CustomCalendarSpec{Kind: ossie.ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "order_date", Grains: []ossie.CustomCalendarGrain{
		{Name: "fiscal_week", BucketDimension: weekBucket.Name, OrdinalDimension: weekOrdinal.Name, ParentGrain: "fiscal_quarter"},
		{Name: "fiscal_quarter", BucketDimension: quarterBucket.Name, OrdinalDimension: quarterOrdinal.Name},
	}}
	grouping := &CustomCalendarGrouping{
		Spec: spec, Grain: query.TimeGrain("fiscal_week"), Dataset: "calendar", DatasetSource: "analytics.calendar",
		BaseTimeField: baseTime, BucketField: weekBucket, BucketExpression: "calendar.fiscal_week_start",
		OrdinalField: weekOrdinal, OrdinalExpression: "calendar.fiscal_week_ordinal",
		Levels: map[string]CustomCalendarLevel{
			"fiscal_week":    {Grain: spec.Grains[0], BucketField: weekBucket, BucketExpression: "calendar.fiscal_week_start", OrdinalField: weekOrdinal, OrdinalExpression: "calendar.fiscal_week_ordinal"},
			"fiscal_quarter": {Grain: spec.Grains[1], BucketField: quarterBucket, BucketExpression: "calendar.fiscal_quarter_start", OrdinalField: quarterOrdinal, OrdinalExpression: "calendar.fiscal_quarter_ordinal"},
		},
	}
	groups := []GroupBy{{Name: "order_date@fiscal_week", CustomCalendar: grouping}}
	metricSpec := ossie.CumulativeMetricSpec{Kind: ossie.MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: ossie.CumulativeWindow{Type: "grain_to_date", Unit: "fiscal_quarter"}}
	nodes := []SemanticPlanNode{CumulativeWindowNode{Base: SemanticPlanNodeBase{ID: "quarter_to_date_revenue", OutputGrain: groups}, Spec: metricSpec}}
	if err := planCustomCalendarGrainToDates(nodes, groups); err != nil {
		t.Fatal(err)
	}
	owned := nodes[0].(CumulativeWindowNode).CustomCalendarGrainToDate
	if owned == nil {
		t.Fatal("custom grain-to-date plan missing")
	}
	planned := *owned
	if planned.QueryGrain != query.TimeGrain("fiscal_week") || planned.ResetGrain != query.TimeGrain("fiscal_quarter") {
		t.Fatalf("plan grains = %#v", planned)
	}
	if planned.ResetBucketField != quarterBucket || planned.ResetBucketExpression != "calendar.fiscal_quarter_start" {
		t.Fatalf("reset bucket = %#v", planned)
	}
	if planned.QueryOrdinalField != weekOrdinal || planned.QueryOrdinalExpression != "calendar.fiscal_week_ordinal" {
		t.Fatalf("query ordinal = %#v", planned)
	}
	if owned.ResetGrain != query.TimeGrain("fiscal_quarter") || owned.Dataset.Name != "calendar" {
		t.Fatalf("stage-owned grain-to-date mapping = %#v", owned)
	}
}

func TestPlanCustomCalendarGrainToDateRejectsQueryAboveReset(t *testing.T) {
	baseTime := &ossie.Field{Name: "order_date"}
	yearBucket := &ossie.Field{Name: "fiscal_year_start"}
	yearOrdinal := &ossie.Field{Name: "fiscal_year_ordinal"}
	quarterBucket := &ossie.Field{Name: "fiscal_quarter_start"}
	quarterOrdinal := &ossie.Field{Name: "fiscal_quarter_ordinal"}
	spec := ossie.CustomCalendarSpec{Kind: ossie.ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "order_date", Grains: []ossie.CustomCalendarGrain{
		{Name: "fiscal_quarter", BucketDimension: quarterBucket.Name, OrdinalDimension: quarterOrdinal.Name, ParentGrain: "fiscal_year"},
		{Name: "fiscal_year", BucketDimension: yearBucket.Name, OrdinalDimension: yearOrdinal.Name},
	}}
	groups := []GroupBy{{Name: "order_date@fiscal_year", CustomCalendar: &CustomCalendarGrouping{
		Spec: spec, Grain: query.TimeGrain("fiscal_year"), Dataset: "calendar", DatasetSource: "analytics.calendar", BaseTimeField: baseTime,
		BucketField: yearBucket, BucketExpression: "calendar.fiscal_year_start", OrdinalField: yearOrdinal, OrdinalExpression: "calendar.fiscal_year_ordinal",
		Levels: map[string]CustomCalendarLevel{
			"fiscal_quarter": {Grain: spec.Grains[0], BucketField: quarterBucket, BucketExpression: "calendar.fiscal_quarter_start", OrdinalField: quarterOrdinal, OrdinalExpression: "calendar.fiscal_quarter_ordinal"},
			"fiscal_year":    {Grain: spec.Grains[1], BucketField: yearBucket, BucketExpression: "calendar.fiscal_year_start", OrdinalField: yearOrdinal, OrdinalExpression: "calendar.fiscal_year_ordinal"},
		},
	}}}
	metric := ossie.CumulativeMetricSpec{Kind: ossie.MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: ossie.CumulativeWindow{Type: "grain_to_date", Unit: "fiscal_quarter"}}
	if _, err := planCustomCalendarGrainToDate("quarter_to_date_revenue", metric, groups); err == nil {
		t.Fatal("expected query grain above reset boundary to fail")
	}
}

func TestPlanCustomCalendarGrainToDatesIgnoreBuiltInUnits(t *testing.T) {
	metricSpec := ossie.CumulativeMetricSpec{Window: ossie.CumulativeWindow{Type: "grain_to_date", Unit: "year"}}
	nodes := []SemanticPlanNode{CumulativeWindowNode{Base: SemanticPlanNodeBase{ID: "ytd"}, Spec: metricSpec}}
	if err := planCustomCalendarGrainToDates(nodes, nil); err != nil {
		t.Fatal(err)
	}
	cumulative := nodes[0].(CumulativeWindowNode)
	if cumulative.CustomCalendarGrainToDate != nil {
		t.Fatal("built-in grain-to-date unexpectedly owns custom mapping")
	}
}
