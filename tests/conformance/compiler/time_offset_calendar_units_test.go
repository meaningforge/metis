package compiler_test

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/query"
)

func TestQuarterAndYearOffsetReadRanges(t *testing.T) {
	cases := []struct {
		name       string
		metric     string
		grain      query.TimeGrain
		visible    []string
		wantSource []string
	}{
		{
			name:       "previous quarter",
			metric:     "previous_quarter_revenue",
			grain:      query.TimeGrainQuarter,
			visible:    []string{"2026-04-01", "2026-12-31"},
			wantSource: []string{"2026-01-01", "2026-12-31"},
		},
		{
			name:       "previous year",
			metric:     "previous_year_revenue",
			grain:      query.TimeGrainYear,
			visible:    []string{"2026-01-01", "2026-12-31"},
			wantSource: []string{"2025-01-01", "2026-12-31"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grain := tc.grain
			q := query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: tc.metric}},
				Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
				Filters:    []query.Filter{{Field: "order_date", Operator: query.FilterBetween, Value: tc.visible}},
			}
			plan, _, err := compile(t, q, "DORIS")
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Nodes) == 0 || len(plan.Output.Predicates) != 1 {
				t.Fatalf("semantic plan nodes = %#v", plan.Nodes)
			}
			source := semanticGraphNodeByID(t, plan.Nodes, "revenue")
			var sourceRange any
			for _, predicate := range semanticGraphNodePredicates(source) {
				if predicate.Filter.Field == "order_date" {
					sourceRange = predicate.Filter.Value
				}
			}
			if !reflect.DeepEqual(sourceRange, tc.wantSource) {
				t.Fatalf("source range = %#v, want %#v", sourceRange, tc.wantSource)
			}
			if !reflect.DeepEqual(plan.Output.Predicates[0].Filter.Value, tc.visible) {
				t.Fatalf("visible range = %#v, want %#v", plan.Output.Predicates[0].Filter.Value, tc.visible)
			}
		})
	}
}
