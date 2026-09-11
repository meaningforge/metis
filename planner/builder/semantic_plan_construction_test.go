package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestRequiresStagedConstructionKeysOnWhatConstructionBuilt(t *testing.T) {
	sourceGroup := func(id, root string) semanticNodeFixture {
		return semanticNodeFixture{
			ID:   id,
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Root: semanticplan.DatasetRef{Name: root},
			Node: semanticplan.SourceAggregateNode{},
		}
	}
	construction := func(stages []semanticNodeFixture) ConstructionInput {
		plan := semanticPlanForTest(semanticplan.SemanticPlan{}, stages)
		return ConstructionInput{
			Nodes: append([]semanticplan.SemanticPlanNode(nil), plan.Nodes...),
		}
	}

	for _, tt := range []struct {
		name  string
		input ConstructionInput
		want  bool
	}{
		{
			name:  "no stages at all",
			input: ConstructionInput{},
			want:  false,
		},
		{
			name:  "one source aggregate over the query root",
			input: construction([]semanticNodeFixture{sourceGroup("revenue", "orders")}),
			want:  false,
		},
		{
			name: "several source aggregates sharing one root",
			input: construction([]semanticNodeFixture{
				sourceGroup("revenue", "orders"),
				sourceGroup("cost", "orders"),
			}),
			want: false,
		},
		{
			name: "source aggregates on different roots",
			input: construction([]semanticNodeFixture{
				sourceGroup("revenue", "orders"),
				sourceGroup("cost", "costs"),
			}),
			want: true,
		},
		{
			name: "an evaluation that is not a source aggregate",
			input: construction([]semanticNodeFixture{
				sourceGroup("revenue", "orders"),
				{ID: "margin", Kind: semanticplan.SemanticPlanNodePostAggregate, Node: semanticplan.PostAggregateNode{}},
			}),
			want: true,
		},
		{
			name: "a final output predicate",
			input: func() ConstructionInput {
				input := construction([]semanticNodeFixture{sourceGroup("revenue", "orders")})
				input.PostEvaluationPredicates = []semanticplan.PostEvaluationPredicate{{Name: "revenue", Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 0}}}
				return input
			}(),
			want: true,
		},
		{
			name: "a metric that declares a definition filter",
			input: construction([]semanticNodeFixture{{
				ID:   "paid_revenue",
				Kind: semanticplan.SemanticPlanNodeSourceAggregate,
				Root: semanticplan.DatasetRef{Name: "orders"},
				Node: semanticplan.SourceAggregateNode{Metric: definitionFilterMetric()},
			}}),
			want: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequiresComposed(tt.input.Nodes, tt.input.PostEvaluationPredicates); got != tt.want {
				t.Fatalf("requiresComposedConstruction = %v, want %v", got, tt.want)
			}
		})
	}
}

func definitionFilterMetric() *ossie.Metric {
	return &ossie.Metric{
		Name: "paid_revenue",
		CustomExtensions: []ossie.CustomExtension{{
			VendorName: "METIS",
			Data:       `{"kind":"definition_filter","filters":[{"field":"status","operator":"eq","value":"paid"}]}`,
		}},
	}
}
