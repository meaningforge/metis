package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestPlanCustomCalendarOffsetUsesTypedOrdinalMapping(t *testing.T) {
	base := &ossie.Field{Name: "day", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{}}
	bucket := &ossie.Field{Name: "fiscal_week_start", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{}}
	ordinal := &ossie.Field{Name: "fiscal_week_index", Datatype: ossie.DataTypeInteger, Dimension: &ossie.Dimension{}}
	grain := query.TimeGrain("fiscal_week")
	plan, err := planCustomCalendarOffset("revenue_prev_fiscal_week", ossie.TimeOffsetMetricSpec{
		TimeDimension: "day",
		Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
	}, []GroupBy{{
		Name: "day",
		CustomCalendar: &CustomCalendarGrouping{
			Grain: grain, Dataset: "calendar", DatasetSource: "analytics.calendar", BaseTimeField: base,
			BucketField: bucket, BucketExpression: "fiscal_week_start",
			OrdinalField: ordinal, OrdinalExpression: "fiscal_week_index",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Grain != grain || plan.Count != -1 || plan.Dataset.Name != "calendar" || plan.Dataset.Source != "analytics.calendar" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.BucketField != bucket || plan.BucketExpression != "fiscal_week_start" {
		t.Fatalf("bucket mapping = %#v", plan)
	}
	if plan.OrdinalField != ordinal || plan.OrdinalExpression != "fiscal_week_index" {
		t.Fatalf("ordinal mapping = %#v", plan)
	}
}

func TestPlanCustomCalendarOffsetsAttachesMappingToTypedNode(t *testing.T) {
	base := &ossie.Field{Name: "day", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{}}
	bucket := &ossie.Field{Name: "fiscal_week_start", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{}}
	ordinal := &ossie.Field{Name: "fiscal_week_index", Datatype: ossie.DataTypeInteger, Dimension: &ossie.Dimension{}}
	grain := query.TimeGrain("fiscal_week")
	nodes := []SemanticPlanNode{TimeOffsetNode{
		Base: SemanticPlanNodeBase{ID: "revenue_prev_fiscal_week", OutputGrain: []GroupBy{{Name: "day", CustomCalendar: &CustomCalendarGrouping{
			Grain: grain, Dataset: "calendar", DatasetSource: "analytics.calendar", BaseTimeField: base,
			BucketField: bucket, BucketExpression: "fiscal_week_start", OrdinalField: ordinal, OrdinalExpression: "fiscal_week_index",
		}}}},
		Spec: ossie.TimeOffsetMetricSpec{
			TimeDimension: "day", Offset: ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
		},
	}}
	if err := planCustomCalendarOffsets(nodes); err != nil {
		t.Fatal(err)
	}
	owned := nodes[0].(TimeOffsetNode).CustomCalendar
	if owned == nil || owned.Dataset.Name != "calendar" || owned.OrdinalExpression != "fiscal_week_index" {
		t.Fatalf("stage-owned mapping = %#v", owned)
	}
}

func TestPlanCustomCalendarOffsetsIgnoresBuiltInUnits(t *testing.T) {
	spec := ossie.TimeOffsetMetricSpec{TimeDimension: "day", Offset: ossie.TimeOffset{Count: -1, Unit: "month"}}
	nodes := []SemanticPlanNode{TimeOffsetNode{Base: SemanticPlanNodeBase{ID: "revenue_prev_month"}, Spec: spec}}
	if err := planCustomCalendarOffsets(nodes); err != nil {
		t.Fatal(err)
	}
	offset := nodes[0].(TimeOffsetNode)
	if offset.CustomCalendar != nil {
		t.Fatal("built-in offset unexpectedly received custom calendar ownership")
	}
}

func TestPlanCustomCalendarOffsetRequiresMatchingCustomQueryGrain(t *testing.T) {
	_, err := planCustomCalendarOffset("revenue_prev_fiscal_week", ossie.TimeOffsetMetricSpec{
		TimeDimension: "day",
		Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
	}, []GroupBy{{Name: "day"}})
	if err == nil {
		t.Fatal("expected missing custom grain planning error")
	}
}

func TestPlanCustomCalendarOffsetRejectsIncompleteMapping(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	base := &ossie.Field{Name: "day", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{}}
	_, err := planCustomCalendarOffset("revenue_prev_fiscal_week", ossie.TimeOffsetMetricSpec{
		TimeDimension: "day",
		Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
	}, []GroupBy{{CustomCalendar: &CustomCalendarGrouping{Grain: grain, Dataset: "calendar", BaseTimeField: base}}})
	if err == nil {
		t.Fatal("expected incomplete mapping planning error")
	}
}
