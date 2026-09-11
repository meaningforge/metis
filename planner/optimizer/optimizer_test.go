package optimizer_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestOptimizerDeduplicatesAndPrunesJoinsWithoutMutatingInput(t *testing.T) {
	ordersToCustomer := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer"}
	customerToProduct := &ossie.Relationship{Name: "customer_to_product", From: "customer", To: "product"}
	input := &semanticplan.SemanticPlan{
		Root: semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Joins: []semanticplan.Join{
			{Relationship: ordersToCustomer, FromDataset: "orders", ToDataset: "customer"},
			{Relationship: ordersToCustomer, FromDataset: "orders", ToDataset: "customer"},
			{Relationship: customerToProduct, FromDataset: "customer", ToDataset: "product"},
		},
		Projections: []semanticplan.Projection{{Name: "region", Kind: semanticplan.ProjectionDimension, Dataset: "customer"}},
	}

	optimized, err := optimizer.Default().Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Joins) != 3 {
		t.Fatalf("optimizer mutated input joins: %#v", input.Joins)
	}
	if len(optimized.Joins) != 1 || optimized.Joins[0].Relationship != ordersToCustomer {
		t.Fatalf("optimized joins = %#v", optimized.Joins)
	}
	wantTrace := []semanticplan.OptimizationStep{
		{Rule: "metric_projection_pruning", Changed: false},
		{Rule: "semantic_set_normalization", Changed: false},
		{Rule: "projection_deduplication", Changed: false},
		{Rule: "predicate_deduplication", Changed: false},
		{Rule: "predicate_pushdown", Changed: false},
		{Rule: "join_deduplication", Changed: true},
		{Rule: "unused_join_elimination", Changed: true},
		{Rule: "source_scan_fusion", Changed: false},
	}
	if !reflect.DeepEqual(optimized.OptimizationTrace, wantTrace) {
		t.Fatalf("optimization trace = %#v, want %#v", optimized.OptimizationTrace, wantTrace)
	}
}

func TestSemanticSetNormalizationEnablesEquivalentSourceFusion(t *testing.T) {
	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "margin", Kind: semanticplan.ProjectionMetric, Datasets: []string{"payments", "orders", "orders"}}},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"},
				Source: semanticplan.SemanticSourceState{
					Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
					RequiredDatasets: []string{"payments", "orders", "payments"},
					SourceRoots:      []string{"orders", "orders"},
				},
			},
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{ID: "refunds"},
				Source: semanticplan.SemanticSourceState{
					Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
					RequiredDatasets: []string{"orders", "payments"},
					SourceRoots:      []string{"orders"},
				},
			},
		},
	}

	optimized, err := optimizer.NewCanonical(optimizer.SemanticSetNormalizationRule{}, optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	wantDatasets := []string{"orders", "payments"}
	if !reflect.DeepEqual(optimized.Projections[0].Datasets, wantDatasets) {
		t.Fatalf("projection datasets = %v, want %v", optimized.Projections[0].Datasets, wantDatasets)
	}
	first := optimized.Nodes[0].(semanticplan.SourceAggregateNode)
	second := optimized.Nodes[1].(semanticplan.SourceAggregateNode)
	if !reflect.DeepEqual(first.Source.RequiredDatasets, wantDatasets) {
		t.Fatalf("required datasets = %v, want %v", first.Source.RequiredDatasets, wantDatasets)
	}
	if got := first.MetricState.ShareGroup; got == "" || second.MetricState.ShareGroup != got {
		t.Fatalf("source nodes were not fused: %#v", optimized.Nodes)
	}
	original := input.Nodes[0].(semanticplan.SourceAggregateNode)
	if !reflect.DeepEqual(input.Projections[0].Datasets, []string{"payments", "orders", "orders"}) || !reflect.DeepEqual(original.Source.RequiredDatasets, []string{"payments", "orders", "payments"}) {
		t.Fatal("optimizer mutated input semantic sets")
	}
}

func TestOptimizerPreservesJoinTreeWhenPlanHasNoConsumers(t *testing.T) {
	relationship := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer"}
	input := &semanticplan.SemanticPlan{Root: semanticplan.DatasetRef{Name: "orders", Source: "orders"}, Joins: []semanticplan.Join{{Relationship: relationship, FromDataset: "orders", ToDataset: "customer"}}}
	optimized, err := optimizer.Default().Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Joins) != 1 {
		t.Fatalf("join-only plan was pruned: %#v", optimized.Joins)
	}
}

func TestMetricProjectionPruningKeepsOnlyOutputDependencyClosure(t *testing.T) {
	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "margin", Kind: semanticplan.ProjectionMetric}},
		Requested:   []string{"margin"},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}, MetricState: semanticplan.SemanticMetricState{ShareGroup: "stale"}},
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "cost"}, MetricState: semanticplan.SemanticMetricState{ShareGroup: "stale"}},
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "unused"}, MetricState: semanticplan.SemanticMetricState{ShareGroup: "stale"}},
			semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "margin", Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}, {NodeID: "cost"}}}},
		},
	}

	optimized, err := optimizer.NewCanonical(optimizer.MetricProjectionPruningRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"revenue", "cost", "margin"}
	got := make([]string, 0, len(optimized.Nodes))
	for _, node := range optimized.Nodes {
		got = append(got, node.NodeBase().ID)
		switch node := node.(type) {
		case semanticplan.SourceAggregateNode:
			if node.MetricState.ShareGroup != "" {
				t.Fatalf("stale share group retained on %q: %q", node.Base.ID, node.MetricState.ShareGroup)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluation nodes = %v, want %v", got, want)
	}
	if len(input.Nodes) != 4 {
		t.Fatalf("optimizer mutated input nodes: %#v", input.Nodes)
	}
}

func TestPredicateRulesDeduplicateAndPushFiltersToEveryLeaf(t *testing.T) {
	predicate := semanticplan.Predicate{Dataset: "orders", Expression: expression.NewResolvedExpression("ANSI_SQL", "status"), Filter: query.Filter{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"}}
	owned := predicate
	input := &semanticplan.SemanticPlan{
		Predicates: []semanticplan.Predicate{predicate, predicate},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:                        "revenue",
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof, Proof: semanticplan.SemanticPredicateProofDatasetReachability},
					Predicates:                []semanticplan.SemanticPlanNodePredicate{{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &owned}},
				},
				Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders"}},
			},
			semanticplan.SourceAggregateNode{
				Base:   semanticplan.SemanticPlanNodeBase{ID: "cost", PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof, Proof: semanticplan.SemanticPredicateProofDatasetReachability}},
				Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders"}},
			},
			semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "margin", Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}, {NodeID: "cost"}}}},
		},
	}

	optimizer := optimizer.NewCanonical(optimizer.PredicateDeduplicationRule{}, optimizer.PredicatePushdownRule{})
	optimized, err := optimizer.Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Predicates) != 0 {
		t.Fatalf("top-level predicates were not consumed: %#v", optimized.Predicates)
	}
	for _, raw := range optimized.Nodes[:2] {
		node := raw.(semanticplan.SourceAggregateNode)
		predicates := node.Base.Predicates
		if len(predicates) != 1 || predicates[0].Predicate == nil || !reflect.DeepEqual(*predicates[0].Predicate, predicate) {
			t.Fatalf("leaf %s predicates = %#v", node.Base.ID, predicates)
		}
	}
	if len(optimized.Nodes[2].NodeBase().Predicates) != 0 {
		t.Fatalf("derived metric received a pre-aggregation predicate: %#v", optimized.Nodes[2])
	}
	inputCost := input.Nodes[1].(semanticplan.SourceAggregateNode)
	if len(input.Predicates) != 2 || len(inputCost.Base.Predicates) != 0 {
		t.Fatal("optimizer mutated predicate input")
	}
}

func TestPredicatePushdownRejectsPartialLeafApplicability(t *testing.T) {
	input := &semanticplan.SemanticPlan{
		Predicates: []semanticplan.Predicate{{Dataset: "customer"}},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base:   semanticplan.SemanticPlanNodeBase{ID: "revenue", PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof, Proof: semanticplan.SemanticPredicateProofDatasetReachability}},
			Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders"}},
		}},
	}
	_, err := optimizer.NewCanonical(optimizer.PredicatePushdownRule{}).Optimize(context.Background(), input)
	if err == nil {
		t.Fatal("expected unreachable predicate error")
	}
}

func advancedPredicatePlan(kind semanticplan.SemanticPlanNodeKind, predicate *semanticplan.Predicate) *semanticplan.SemanticPlan {
	sourceBase := semanticplan.SemanticPlanNodeBase{
		ID:       "revenue",
		Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
			Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
			Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
		},
	}
	if predicate != nil {
		owned := *predicate
		sourceBase.Predicates = []semanticplan.SemanticPlanNodePredicate{{
			Scope:       semanticplan.SemanticPredicatePreAggregation,
			OwnerNodeID: "revenue",
			Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
			Predicate:   &owned,
		}}
	}
	source := semanticplan.SourceAggregateNode{
		Base:   sourceBase,
		Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders"}},
	}
	advancedBase := semanticplan.SemanticPlanNodeBase{
		ID:       "advanced_revenue",
		Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
		Inputs:   []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}},
		PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
			Movement: semanticplan.SemanticPredicateBoundaryBlocked,
			Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
		},
	}
	var advanced semanticplan.SemanticPlanNode
	switch kind {
	case semanticplan.SemanticPlanNodeCumulativeWindow:
		advanced = semanticplan.CumulativeWindowNode{Base: advancedBase}
	case semanticplan.SemanticPlanNodeTimeOffset:
		advanced = semanticplan.TimeOffsetNode{Base: advancedBase}
	case semanticplan.SemanticPlanNodeOffsetToGrain:
		advanced = semanticplan.OffsetToGrainNode{Base: advancedBase}
	case semanticplan.SemanticPlanNodeConversion:
		advanced = semanticplan.ConversionNode{Base: advancedBase}
	case semanticplan.SemanticPlanNodeSemiAdditiveLast:
		advanced = semanticplan.SemiAdditiveNode{Base: advancedBase, Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "last"}}
	default:
		panic("unsupported advanced predicate node kind: " + string(kind))
	}
	return &semanticplan.SemanticPlan{
		Requested: []string{"advanced_revenue"},
		Nodes:     []semanticplan.SemanticPlanNode{source, advanced},
	}
}

func TestPredicatePushdownRequiresPlannerProofAcrossAdvancedNodes(t *testing.T) {
	predicate := semanticplan.Predicate{
		Dataset:    "orders",
		Expression: expression.NewResolvedExpression("ANSI_SQL", "status"),
		Filter:     query.Filter{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"},
	}
	advancedKinds := []semanticplan.SemanticPlanNodeKind{
		semanticplan.SemanticPlanNodeCumulativeWindow,
		semanticplan.SemanticPlanNodeTimeOffset,
		semanticplan.SemanticPlanNodeOffsetToGrain,
		semanticplan.SemanticPlanNodeConversion,
		semanticplan.SemanticPlanNodeSemiAdditiveLast,
	}

	for _, kind := range advancedKinds {
		t.Run(string(kind), func(t *testing.T) {
			withProof := advancedPredicatePlan(kind, &predicate)
			withProof.Predicates = []semanticplan.Predicate{predicate}

			optimized, err := optimizer.NewCanonical(optimizer.PredicatePushdownRule{}).Optimize(context.Background(), withProof)
			if err != nil {
				t.Fatal(err)
			}
			if len(optimized.Predicates) != 0 {
				t.Fatalf("top-level predicate retained after planner-owned placement proof: %#v", optimized.Predicates)
			}
			source := optimized.Nodes[0].(semanticplan.SourceAggregateNode)
			if got := source.Base.Predicates; len(got) != 1 || got[0].Predicate == nil || !reflect.DeepEqual(*got[0].Predicate, predicate) {
				t.Fatalf("source predicate placement changed: %#v", got)
			}
			inputSource := withProof.Nodes[0].(semanticplan.SourceAggregateNode)
			if len(withProof.Predicates) != 1 || len(inputSource.Base.Predicates) != 1 {
				t.Fatal("optimizer mutated advanced predicate-placement input")
			}

			withoutProof := advancedPredicatePlan(kind, nil)
			withoutProof.Predicates = []semanticplan.Predicate{predicate}
			unchanged, err := optimizer.NewCanonical(optimizer.PredicatePushdownRule{}).Optimize(context.Background(), withoutProof)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(unchanged.Predicates, []semanticplan.Predicate{predicate}) {
				t.Fatalf("optimizer consumed predicate without planner proof: %#v", unchanged.Predicates)
			}
			unchangedSource := unchanged.Nodes[0].(semanticplan.SourceAggregateNode)
			if len(unchangedSource.Base.Predicates) != 0 {
				t.Fatalf("optimizer invented advanced source placement: %#v", unchangedSource.Base.Predicates)
			}
		})
	}
}

func TestPredicatePushdownFailsClosedOnUnreachableDataset(t *testing.T) {
	predicate := semanticplan.Predicate{
		Dataset:    "customers",
		Expression: expression.NewResolvedExpression("ANSI_SQL", "region"),
		Filter:     query.Filter{Field: "customers.region", Operator: query.FilterEQ, Value: "APAC"},
	}
	input := &semanticplan.SemanticPlan{
		Requested:  []string{"revenue"},
		Predicates: []semanticplan.Predicate{predicate},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:       "revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
					Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
				},
			},
			Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders"}},
		}},
	}

	_, err := optimizer.NewCanonical(optimizer.PredicatePushdownRule{}).Optimize(context.Background(), input)
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrInternalInvariant {
		t.Fatalf("ordinary unreachable predicate error = %v, want an internal invariant violation", err)
	}
	if metisErr.Details["dataset"] != "customers" {
		t.Fatalf("details = %v, want the unreachable dataset reported as customers", metisErr.Details)
	}
	inputSource := input.Nodes[0].(semanticplan.SourceAggregateNode)
	if len(input.Predicates) != 1 || len(inputSource.Base.Predicates) != 0 {
		t.Fatal("failed ordinary pushdown mutated caller-owned input")
	}
}

func TestAdvancedPredicatePlacementIsStableAcrossDefaultOptimizerFixpoint(t *testing.T) {
	predicate := semanticplan.Predicate{
		Dataset:    "orders",
		Expression: expression.NewResolvedExpression("ANSI_SQL", "status"),
		Filter:     query.Filter{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"},
	}
	input := advancedPredicatePlan(semanticplan.SemanticPlanNodeCumulativeWindow, &predicate)
	source := input.Nodes[0].(semanticplan.SourceAggregateNode)
	duplicate := source.Base.Predicates[0]
	source.Base.Predicates = append(source.Base.Predicates, duplicate)
	source.Source.RequiredDatasets = []string{"orders", "orders"}
	source.Source.SourceRoots = []string{"orders", "orders"}
	input.Nodes[0] = source
	input.Predicates = []semanticplan.Predicate{predicate, predicate}

	optimized, err := optimizer.Default().Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Predicates) != 0 {
		t.Fatalf("proved advanced predicate was not consumed: %#v", optimized.Predicates)
	}
	optimizedSource := optimized.Nodes[0].(semanticplan.SourceAggregateNode)
	if got := optimizedSource.Base.Predicates; len(got) != 1 || got[0].Predicate == nil || !reflect.DeepEqual(*got[0].Predicate, predicate) {
		t.Fatalf("advanced source predicates = %#v, want one canonical predicate", got)
	}
	firstFingerprint, err := semanticplan.Fingerprint(optimized)
	if err != nil {
		t.Fatal(err)
	}

	optimizedAgain, err := optimizer.Default().Optimize(context.Background(), optimized)
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := semanticplan.Fingerprint(optimizedAgain)
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("advanced predicate placement fingerprint changed across fixpoint: first=%s second=%s", firstFingerprint, secondFingerprint)
	}
	for _, step := range optimizedAgain.OptimizationTrace {
		if step.Changed {
			t.Fatalf("normalized advanced predicate plan changed on re-optimization: %#v", optimizedAgain.OptimizationTrace)
		}
	}
	inputSource := input.Nodes[0].(semanticplan.SourceAggregateNode)
	if len(input.Predicates) != 2 || len(inputSource.Base.Predicates) != 2 {
		t.Fatal("default optimizer mutated advanced predicate input")
	}
}
