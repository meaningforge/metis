package compiler_test

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestTimeOffsetPlanningContract(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "previous_month_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
	}
	plan, _, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("semantic nodes = %d, want revenue + previous_month_revenue", len(plan.Nodes))
	}
	base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !ok || base.Base.ID != "revenue" {
		t.Fatalf("base node = %#v, want revenue source aggregate", plan.Nodes[0])
	}
	offset, ok := plan.Nodes[1].(semanticplan.TimeOffsetNode)
	if !ok || offset.Base.ID != "previous_month_revenue" {
		t.Fatalf("offset node = %#v", plan.Nodes[1])
	}
	spec := offset.Spec
	if spec.BaseMetric != "revenue" || spec.TimeDimension != "order_date" || spec.Offset.Count != -1 || spec.Offset.Unit != "month" {
		t.Fatalf("time-offset spec = %#v", spec)
	}
}

func TestTimeOffsetRequiresMatchingQueryGrain(t *testing.T) {
	grain := query.TimeGrainQuarter
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "previous_month_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
	}
	_, _, err := compile(t, q, "DORIS")
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrIncompatibleQueryGrain)
	}
}
