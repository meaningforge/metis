package manifest

import (
	"testing"

	"github.com/meaningforge/metis/expression"
)

func TestMetricDecompositionRequiresExactlyOneAdditivePartialState(t *testing.T) {
	tests := []struct {
		name  string
		state []expression.PartialStateComponent
	}{
		{name: "missing", state: nil},
		{
			name: "multiple",
			state: []expression.PartialStateComponent{
				{Name: expression.PartialStateSum},
				{Name: expression.PartialStateCount},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := call("SUM", ident("amount"))
			analyzed := analyzedExpression("ANSI_SQL", expr, nil)
			analyzed.AggregationProperties[0].PartialState = tt.state
			model := modelWithAnalyses(map[string]MetricAnalysis{
				"metric": {Expressions: []AnalyzedExpression{analyzed}},
			})

			if got := model.MetricDecomposition("metric").Kind(); got != MetricDecompositionUnsupported {
				t.Fatalf("decomposition = %q, want unsupported", got)
			}
		})
	}
}
