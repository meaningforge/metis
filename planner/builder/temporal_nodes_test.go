package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestPlanOffsetToGrainBuiltInBoundary(t *testing.T) {
	month := query.TimeGrainMonth
	field := &ossie.Field{Name: "order_date"}
	plan, err := PlanOffsetToGrain("revenue_at_start_of_year", ossie.OffsetToGrainMetricSpec{
		Kind: ossie.MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "year",
	}, []GroupBy{{Name: "order_date@month", Field: field, Grain: &month}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.QueryGrain != query.TimeGrainMonth || plan.BoundaryGrain != query.TimeGrainYear || plan.CustomCalendar {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanOffsetToGrainAllowsEqualBoundary(t *testing.T) {
	year := query.TimeGrainYear
	field := &ossie.Field{Name: "order_date"}
	if _, err := PlanOffsetToGrain("revenue_at_start_of_year", ossie.OffsetToGrainMetricSpec{
		Kind: ossie.MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "year",
	}, []GroupBy{{Name: "order_date@year", Field: field, Grain: &year}}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanOffsetToGrainRejectsCoarserQueryGrain(t *testing.T) {
	year := query.TimeGrainYear
	field := &ossie.Field{Name: "order_date"}
	if _, err := PlanOffsetToGrain("revenue_at_start_of_month", ossie.OffsetToGrainMetricSpec{
		Kind: ossie.MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "month",
	}, []GroupBy{{Name: "order_date@year", Field: field, Grain: &year}}); err == nil {
		t.Fatal("expected coarser query grain error")
	}
}

func TestPlanOffsetToGrainRequiresExplicitTimeGrain(t *testing.T) {
	field := &ossie.Field{Name: "order_date"}
	if _, err := PlanOffsetToGrain("revenue_at_start_of_year", ossie.OffsetToGrainMetricSpec{
		Kind: ossie.MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "year",
	}, []GroupBy{{Name: "order_date", Field: field}}); err == nil {
		t.Fatal("expected explicit query grain error")
	}
}

func TestPlanOffsetToGrainCustomHierarchy(t *testing.T) {
	base := &ossie.Field{Name: "order_date"}
	weekBucket := &ossie.Field{Name: "fiscal_week"}
	weekOrdinal := &ossie.Field{Name: "fiscal_week_ordinal"}
	yearBucket := &ossie.Field{Name: "fiscal_year"}
	spec := ossie.CustomCalendarSpec{Kind: ossie.ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "order_date", Grains: []ossie.CustomCalendarGrain{
		{Name: "fiscal_week", BucketDimension: "fiscal_week", OrdinalDimension: "fiscal_week_ordinal", ParentGrain: "fiscal_year"},
		{Name: "fiscal_year", BucketDimension: "fiscal_year", OrdinalDimension: "fiscal_year_ordinal"},
	}}
	calendar := &CustomCalendarGrouping{
		Spec: spec, Grain: query.TimeGrain("fiscal_week"), Dataset: "calendar", DatasetSource: "analytics.calendar",
		BaseTimeField: base, BucketField: weekBucket, OrdinalField: weekOrdinal, OrdinalExpression: "calendar.fiscal_week_ordinal",
		Levels: map[string]CustomCalendarLevel{
			"fiscal_year": {Grain: spec.Grains[1], BucketField: yearBucket, BucketExpression: "calendar.fiscal_year"},
		},
	}
	plan, err := PlanOffsetToGrain("revenue_at_start_of_fiscal_year", ossie.OffsetToGrainMetricSpec{
		Kind: ossie.MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "fiscal_year",
	}, []GroupBy{{Name: "order_date@fiscal_week", Field: base, CustomCalendar: calendar}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CustomCalendar || plan.BoundaryBucket != yearBucket || plan.QueryOrdinal != weekOrdinal || plan.Dataset.Name != "calendar" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanOffsetToGrainRejectsUnrelatedCustomBoundary(t *testing.T) {
	base := &ossie.Field{Name: "order_date"}
	calendar := &CustomCalendarGrouping{
		Spec: ossie.CustomCalendarSpec{Kind: ossie.ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "order_date", Grains: []ossie.CustomCalendarGrain{
			{Name: "fiscal_week", BucketDimension: "fiscal_week", OrdinalDimension: "fiscal_week_ordinal"},
			{Name: "fiscal_year", BucketDimension: "fiscal_year", OrdinalDimension: "fiscal_year_ordinal"},
		}},
		Grain: query.TimeGrain("fiscal_week"), Dataset: "calendar", DatasetSource: "analytics.calendar", BaseTimeField: base,
	}
	if _, err := PlanOffsetToGrain("revenue_at_start_of_fiscal_year", ossie.OffsetToGrainMetricSpec{
		Kind: ossie.MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "fiscal_year",
	}, []GroupBy{{Name: "order_date@fiscal_week", Field: base, CustomCalendar: calendar}}); err == nil {
		t.Fatal("expected unrelated custom hierarchy error")
	}
}

func TestValidateTimeOffsetQueryGrain(t *testing.T) {
	month := query.TimeGrainMonth
	field := &ossie.Field{Name: "order_date"}
	if err := ValidateTimeOffsetQueryGrain("previous_month_revenue", ossie.TimeOffsetMetricSpec{
		TimeDimension: "order_date",
		Offset:        ossie.TimeOffset{Count: -1, Unit: "month"},
	}, []GroupBy{{Name: "order_date@month", Field: field, Grain: &month}}); err != nil {
		t.Fatalf("valid built-in time offset was rejected: %v", err)
	}
	if err := ValidateTimeOffsetQueryGrain("previous_fiscal_week_revenue", ossie.TimeOffsetMetricSpec{
		TimeDimension: "order_date",
		Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
	}, []GroupBy{{Name: "order_date@month", Field: field, Grain: &month}}); err == nil {
		t.Fatal("custom time offset without matching custom grain was accepted")
	}
}
