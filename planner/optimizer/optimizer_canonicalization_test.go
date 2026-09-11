package optimizer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

type canonicalizationProbeRule struct {
	t *testing.T
}

func (canonicalizationProbeRule) Name() string { return "canonicalization_probe" }

func (r canonicalizationProbeRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	r.t.Helper()
	wantDatasets := []string{"orders", "payments"}
	if !reflect.DeepEqual(plan.Projections[0].Datasets, wantDatasets) {
		r.t.Fatalf("first rewrite saw projection datasets %v, want canonical %v", plan.Projections[0].Datasets, wantDatasets)
	}
	node := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !reflect.DeepEqual(node.Source.RequiredDatasets, wantDatasets) {
		r.t.Fatalf("first rewrite saw required datasets %v, want canonical %v", node.Source.RequiredDatasets, wantDatasets)
	}
	if !reflect.DeepEqual(node.Source.SourceRoots, []string{"orders"}) {
		r.t.Fatalf("first rewrite saw source roots %v", node.Source.SourceRoots)
	}
	return false, nil
}

func TestOptimizerCanonicalizesBeforeFirstRewrite(t *testing.T) {
	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{
			Name:     "margin",
			Kind:     semanticplan.ProjectionMetric,
			Datasets: []string{"payments", "orders", "orders"},
		}},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"},
			Source: semanticplan.SemanticSourceState{
				RequiredDatasets: []string{"payments", "orders", "payments"},
				SourceRoots:      []string{"orders", "orders"},
			},
		}},
	}

	optimized, err := optimizer.NewCanonical(canonicalizationProbeRule{t: t}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got := optimized.OptimizationTrace; !reflect.DeepEqual(got, []semanticplan.OptimizationStep{{Rule: "canonicalization_probe", Changed: false}}) {
		t.Fatalf("optimization trace = %#v", got)
	}
	if !reflect.DeepEqual(input.Projections[0].Datasets, []string{"payments", "orders", "orders"}) {
		t.Fatalf("optimizer mutated input projection datasets: %v", input.Projections[0].Datasets)
	}
	inputNode := input.Nodes[0].(semanticplan.SourceAggregateNode)
	if !reflect.DeepEqual(inputNode.Source.RequiredDatasets, []string{"payments", "orders", "payments"}) {
		t.Fatalf("optimizer mutated input required datasets: %v", inputNode.Source.RequiredDatasets)
	}
}

func TestOptimizerCanonicalizationStabilizesEquivalentRewriteFingerprints(t *testing.T) {
	makePlan := func(firstRequired, secondRequired []string, projectionDatasets []string) *semanticplan.SemanticPlan {
		return &semanticplan.SemanticPlan{
			Requested: []string{"revenue", "refunds"},
			Projections: []semanticplan.Projection{{
				Name:     "margin",
				Kind:     semanticplan.ProjectionMetric,
				Datasets: projectionDatasets,
			}},
			Nodes: []semanticplan.SemanticPlanNode{
				semanticplan.SourceAggregateNode{
					Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"},
					Source: semanticplan.SemanticSourceState{
						Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
						RequiredDatasets: firstRequired,
						SourceRoots:      []string{"orders", "orders"},
					},
				},
				semanticplan.SourceAggregateNode{
					Base: semanticplan.SemanticPlanNodeBase{ID: "refunds"},
					Source: semanticplan.SemanticSourceState{
						Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
						RequiredDatasets: secondRequired,
						SourceRoots:      []string{"orders"},
					},
				},
			},
		}
	}

	left := makePlan(
		[]string{"payments", "orders", "payments"},
		[]string{"orders", "payments"},
		[]string{"payments", "orders", "orders"},
	)
	right := makePlan(
		[]string{"orders", "payments"},
		[]string{"payments", "orders", "orders"},
		[]string{"orders", "payments"},
	)

	optimizer := optimizer.NewCanonical(optimizer.SourceScanFusionRule{})
	leftOptimized, err := optimizer.Optimize(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	rightOptimized, err := optimizer.Optimize(context.Background(), right)
	if err != nil {
		t.Fatal(err)
	}
	for name, plan := range map[string]*semanticplan.SemanticPlan{"left": leftOptimized, "right": rightOptimized} {
		if len(plan.Nodes) != 2 {
			t.Fatalf("%s source nodes = %#v", name, plan.Nodes)
		}
		first := plan.Nodes[0].(semanticplan.SourceAggregateNode)
		second := plan.Nodes[1].(semanticplan.SourceAggregateNode)
		if first.MetricState.ShareGroup != "source_001" || second.MetricState.ShareGroup != "source_001" {
			t.Fatalf("%s source nodes = %#v", name, plan.Nodes)
		}
	}

	leftFingerprint, err := semanticplan.Fingerprint(leftOptimized)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(rightOptimized)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("equivalent optimized fingerprints differ: left=%s right=%s", leftFingerprint, rightFingerprint)
	}
}

// The optimizer's output must own its DAG outright.
func TestOptimizerOutputOwnsItsNodesRatherThanTheCallers(t *testing.T) {
	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "revenue", Kind: semanticplan.ProjectionMetric}},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:       "revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
					Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
				},
			},
			Source: semanticplan.SemanticSourceState{
				RequiredDatasets: []string{"orders"},
				SourceRoots:      []string{"orders"},
			},
		}},
	}
	beforeNode := input.Nodes[0].(semanticplan.SourceAggregateNode)

	optimized, err := optimizer.NewCanonical().Optimize(context.Background(), input)
	if err != nil {
		t.Fatalf("typed-node optimization failed: %v", err)
	}
	optimizedNode := optimized.Nodes[0].(semanticplan.SourceAggregateNode)
	optimizedNode.Source.RequiredDatasets[0] = "mutated"
	if got := input.Nodes[0].(semanticplan.SourceAggregateNode).Source.RequiredDatasets[0]; got != "orders" {
		t.Fatalf("optimizer output shares node-owned dataset storage with input: %q", got)
	}
	if !reflect.DeepEqual(input.Nodes[0], beforeNode) {
		t.Fatal("optimizer mutated the caller's node DAG")
	}
}
