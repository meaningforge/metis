package builder

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func ownedSourceStage(id string) semanticNodeFixture {
	return semanticNodeFixture{
		ID:       id,
		Kind:     semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary: semanticNodeFixtureBoundaryForKind(semanticplan.SemanticPlanNodeSourceAggregate),
		Metrics:  []string{id},
		Node:     semanticplan.SourceAggregateNode{},
	}
}

func TestPlanOwnedDAGValidationProvesTheDAG(t *testing.T) {
	derived := ownedSourceStage("revenue_per_order")
	derived.Kind = semanticplan.SemanticPlanNodePostAggregate
	derived.Boundary = semanticNodeFixtureBoundaryForKind(semanticplan.SemanticPlanNodePostAggregate)
	derived.Node = semanticplan.PostAggregateNode{}
	derived.Metrics = []string{"revenue_per_order"}
	derived.Inputs = []semanticNodeInputFixture{{NodeID: "revenue"}}

	valid := semanticPlanForTest(semanticplan.SemanticPlan{
		Requested: []string{"revenue_per_order"},
	}, []semanticNodeFixture{ownedSourceStage("revenue"), derived})
	if err := semanticplan.ValidateOwnedDAG(valid); err != nil {
		t.Fatalf("a well-formed plan DAG was rejected: %v", err)
	}

	for _, invalid := range []struct {
		name string
		plan func() *semanticplan.SemanticPlan
		want string
	}{
		{
			name: "duplicate stage",
			plan: func() *semanticplan.SemanticPlan {
				return semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{ownedSourceStage("revenue"), ownedSourceStage("revenue")})
			},
			want: "duplicated",
		},
		{
			name: "empty id",
			plan: func() *semanticplan.SemanticPlan {
				return semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{ownedSourceStage("")})
			},
			want: "empty id",
		},
		{
			// Forward, not merely absent. A membership check alone would read
			// a cycle or a misordering as valid so long as the producer
			// existed somewhere in the list.
			name: "input names a later stage",
			plan: func() *semanticplan.SemanticPlan {
				first := ownedSourceStage("revenue")
				first.Inputs = []semanticNodeInputFixture{{NodeID: "orders"}}
				return semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{first, ownedSourceStage("orders")})
			},
			want: "not an earlier node",
		},
		{
			name: "requested output nothing produces",
			plan: func() *semanticplan.SemanticPlan {
				return semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"margin"}}, []semanticNodeFixture{ownedSourceStage("revenue")})
			},
			want: "no node produces",
		},
		{
			name: "nil typed node",
			plan: func() *semanticplan.SemanticPlan {
				return &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{nil}}
			},
			want: "required",
		},
		{
			name: "unsupported kind",
			plan: func() *semanticplan.SemanticPlan {
				stage := ownedSourceStage("revenue")
				stage.Boundary = ""
				return semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{stage})
			},
			want: "unsupported kind",
		},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			err := semanticplan.ValidateOwnedDAG(invalid.plan())
			if err == nil {
				t.Fatalf("%s was accepted", invalid.name)
			}
			if !strings.Contains(err.Error(), invalid.want) {
				t.Errorf("error = %q, want it to mention %q", err, invalid.want)
			}
		})
	}
}

// A node's outputs are what it emits, not what it is called. A metric node's
// id is its metric so the two coincide there, which is exactly why the rule is
// easy to get wrong for a source selection: it emits dimensions and evaluates
// no metric at all.
func TestRequestedOutputsAreProvenAgainstWhatStagesEmit(t *testing.T) {
	selection := semanticNodeFixture{
		ID:         "source_selection",
		Kind:       semanticplan.SemanticPlanNodeSourceSelection,
		Boundary:   semanticNodeFixtureBoundaryForKind(semanticplan.SemanticPlanNodeSourceSelection),
		Dimensions: []string{"region", "country"},
		Node:       semanticplan.SourceSelectionNode{Mode: semanticplan.SourceSelectionGroupedValues},
	}
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"region", "country"}}, []semanticNodeFixture{selection})
	if err := semanticplan.ValidateOwnedDAG(plan); err != nil {
		t.Fatalf("dimensions a source selection emits were not accepted as produced outputs: %v", err)
	}

	plan.Requested = []string{"region", "revenue"}
	if err := semanticplan.ValidateOwnedDAG(plan); err == nil {
		t.Error("a requested output no node emits was accepted")
	}
}

// Construction installs the plan's nodes once and the plan is the only owner
// from then on. The ownership contract is that what construction hands over is
// copied, not shared.
func TestSetPlanOwnedDAGInstallsStagesThePlanOwns(t *testing.T) {
	built := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{ownedSourceStage("revenue")}).Nodes
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"stale"}}, []semanticNodeFixture{ownedSourceStage("stale")})
	if err := semanticplan.SetOwnedDAG(plan, []string{"revenue"}, built, nil, nil); err != nil {
		t.Fatal(err)
	}

	if len(plan.Nodes) != 1 || plan.Nodes[0].NodeBase().ID != "revenue" {
		t.Fatalf("plan nodes = %#v, want one installed node named revenue", plan.Nodes)
	}
	if strings.Join(plan.Requested, ",") != "revenue" {
		t.Errorf("requested = %v, want the installed request list", plan.Requested)
	}

	// Copied, not shared: rewriting the plan-owned node must not rewrite the
	// construction list that was handed to semanticplan.SetOwnedDAG.
	base := plan.Nodes[0].NodeBase()
	base.ID = "mutated"
	mutated, err := semanticplan.WithNodeBase(plan.Nodes[0], base)
	if err != nil {
		t.Fatal(err)
	}
	plan.Nodes[0] = mutated
	if built[0].NodeBase().ID != "revenue" {
		t.Error("the plan's nodes share storage with the list they were built from")
	}
}
