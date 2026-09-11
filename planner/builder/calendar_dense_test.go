package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestPlanCustomDenseCalendarFromOffsetDomain(t *testing.T) {
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	ordinal := &ossie.Field{Name: "fiscal_week_index"}
	nodes := []SemanticPlanNode{TimeOffsetNode{Base: SemanticPlanNodeBase{ID: "previous_fiscal_week_revenue"}, CustomCalendar: &CustomCalendarOffsetPlan{
		Grain:             query.TimeGrain("fiscal_week"),
		Count:             -1,
		Dataset:           DatasetRef{Name: "calendar", Source: "analytics.calendar"},
		BucketField:       bucket,
		BucketExpression:  "fiscal_week_start",
		OrdinalField:      ordinal,
		OrdinalExpression: "fiscal_week_index",
	},
	}}
	groups := []GroupBy{{
		Name:    "day",
		Dataset: "calendar",
		Field:   bucket,
		CustomCalendar: &CustomCalendarGrouping{
			Grain:             query.TimeGrain("fiscal_week"),
			Dataset:           "calendar",
			DatasetSource:     "analytics.calendar",
			BucketField:       bucket,
			BucketExpression:  "fiscal_week_start",
			OrdinalField:      ordinal,
			OrdinalExpression: "fiscal_week_index",
		},
	}}

	plan, err := planCustomDenseCalendar(nodes, groups)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected custom dense calendar plan")
	}
	if plan.Dataset.Name != "calendar" || plan.Grain != query.TimeGrain("fiscal_week") || plan.QueryTimeDimension != "day" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.BucketField != bucket || plan.OrdinalField != ordinal {
		t.Fatalf("plan lost typed bucket/ordinal handles: %#v", plan)
	}
}

func TestPlanCustomDenseCalendarFromCumulativeDomain(t *testing.T) {
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	ordinal := &ossie.Field{Name: "fiscal_week_index"}
	nodes := []SemanticPlanNode{CumulativeWindowNode{Base: SemanticPlanNodeBase{ID: "rolling_revenue"}, CustomCalendarRolling: &CustomCalendarCumulativePlan{
		Grain:             query.TimeGrain("fiscal_week"),
		Count:             3,
		Dataset:           DatasetRef{Name: "calendar", Source: "analytics.calendar"},
		BucketField:       bucket,
		BucketExpression:  "fiscal_week_start",
		OrdinalField:      ordinal,
		OrdinalExpression: "fiscal_week_index",
	},
	}}
	groups := []GroupBy{{
		Name:    "day",
		Dataset: "calendar",
		Field:   bucket,
		CustomCalendar: &CustomCalendarGrouping{
			Grain:             query.TimeGrain("fiscal_week"),
			Dataset:           "calendar",
			DatasetSource:     "analytics.calendar",
			BucketField:       bucket,
			BucketExpression:  "fiscal_week_start",
			OrdinalField:      ordinal,
			OrdinalExpression: "fiscal_week_index",
		},
	}}

	plan, err := planCustomDenseCalendar(nodes, groups)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected cumulative custom dense calendar plan")
	}
	if plan.Dataset.Name != "calendar" || plan.Grain != query.TimeGrain("fiscal_week") || plan.QueryTimeDimension != "day" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.BucketField != bucket || plan.OrdinalField != ordinal {
		t.Fatalf("plan lost typed bucket/ordinal handles: %#v", plan)
	}
}

func TestPlanCustomDenseCalendarFromOffsetToGrainDomain(t *testing.T) {
	base := &ossie.Field{Name: "order_date"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	ordinal := &ossie.Field{Name: "fiscal_week_index"}
	calendar := &CustomCalendarGrouping{
		Grain: query.TimeGrain("fiscal_week"), Dataset: "calendar", DatasetSource: "analytics.calendar",
		BaseTimeField: base, BucketField: bucket, BucketExpression: "calendar.fiscal_week_start",
		OrdinalField: ordinal, OrdinalExpression: "calendar.fiscal_week_index",
	}
	boundary := &OffsetToGrainPlan{TimeDimension: "order_date", QueryGrain: query.TimeGrain("fiscal_week"), BoundaryGrain: query.TimeGrain("fiscal_year"), CustomCalendar: true, Dataset: DatasetRef{Name: "calendar", Source: "analytics.calendar"}, OrdinalExpression: "calendar.fiscal_week_index"}
	nodes := []SemanticPlanNode{
		SourceAggregateNode{Base: SemanticPlanNodeBase{ID: "revenue"}},
		OffsetToGrainNode{Base: SemanticPlanNodeBase{ID: "revenue_at_fiscal_year_start"}, OffsetPlan: boundary},
	}
	groups := []GroupBy{{Name: "order_date@fiscal_week", Field: base, CustomCalendar: calendar}}
	plan, err := planCustomDenseCalendar(nodes, groups)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.Dataset.Name != "calendar" || plan.Grain != query.TimeGrain("fiscal_week") || plan.BucketField != bucket || plan.OrdinalField != ordinal {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanCustomDenseCalendarRejectsMultipleDomains(t *testing.T) {
	bucket := &ossie.Field{Name: "bucket"}
	ordinal := &ossie.Field{Name: "ordinal"}
	nodes := []SemanticPlanNode{
		TimeOffsetNode{Base: SemanticPlanNodeBase{ID: "a"}, CustomCalendar: &CustomCalendarOffsetPlan{
			Grain: query.TimeGrain("fiscal_week"), Dataset: DatasetRef{Name: "calendar", Source: "analytics.calendar"}, BucketField: bucket, OrdinalField: ordinal,
		}},
		TimeOffsetNode{Base: SemanticPlanNodeBase{ID: "b"}, CustomCalendar: &CustomCalendarOffsetPlan{
			Grain: query.TimeGrain("retail_period"), Dataset: DatasetRef{Name: "calendar", Source: "analytics.calendar"}, BucketField: bucket, OrdinalField: ordinal,
		}},
	}
	groups := []GroupBy{
		{Name: "day", CustomCalendar: &CustomCalendarGrouping{Grain: query.TimeGrain("fiscal_week"), Dataset: "calendar", BucketField: bucket, OrdinalField: ordinal}},
		{Name: "day", CustomCalendar: &CustomCalendarGrouping{Grain: query.TimeGrain("retail_period"), Dataset: "calendar", BucketField: bucket, OrdinalField: ordinal}},
	}

	if _, err := planCustomDenseCalendar(nodes, groups); err == nil {
		t.Fatal("expected multiple custom dense-calendar domains to fail in v1")
	}
}
