package builder

import (
	"fmt"
	"testing"
	"time"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func additiveAttributionRequestForTest() attribution.ResolvedMetricAttributionRequest {
	return attribution.ResolvedMetricAttributionRequest{
		ProjectID:        "analytics",
		MetricRef:        "revenue",
		TimeDimensionRef: "order_time",
		Baseline: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		Current: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		Dimensions: []string{"country"},
	}
}

func additiveAttributionPlanForTest() attribution.MetricAttributionPlan {
	return attribution.MetricAttributionPlan{
		Metric:    "revenue",
		Exactness: attribution.MetricAttributionExact,
		Strategy:  semanticplan.MetricAttributionAdditiveContribution,
		Components: []attribution.MetricAttributionComponent{{
			Metric: "revenue",
			Role:   attribution.MetricAttributionValue,
		}},
		Reconciliation: semanticplan.MetricAttributionReconcileSegmentDelta,
	}
}

func additiveAttributionTimeDimensionForTest() semanticplan.GroupBy {
	return semanticplan.GroupBy{
		Name:    "order_time",
		Dataset: "orders",
		Expression: expression.ResolvedExpression{
			SourceDialect: "ANSI_SQL",
			Source:        "orders.order_time",
		},
	}
}

func buildAdditiveAttributionNodeForTest(t *testing.T) (semanticplan.GroupBy, semanticplan.AdditiveAttributionNode) {
	t.Helper()
	group := semanticplan.GroupBy{Name: "country", Dataset: "orders"}
	node, err := attribution.BuildAdditiveAttributionNode(
		"revenue__attribution__country",
		additiveAttributionRequestForTest(),
		additiveAttributionPlanForTest(),
		semanticplan.SemanticPlanNodeInput{NodeID: "revenue", Grain: []semanticplan.GroupBy{group}},
		group,
		additiveAttributionTimeDimensionForTest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return group, node
}

func additiveAttributionSemanticPlanForTest(t *testing.T) *semanticplan.SemanticPlan {
	t.Helper()
	group, attribution := buildAdditiveAttributionNodeForTest(t)
	producer := semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        "revenue",
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
			Dimensions:                []string{"country"},
			OutputGrain:               []semanticplan.GroupBy{group},
		},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
	}
	return &semanticplan.SemanticPlan{
		Requested: []string{"revenue"},
		Nodes:     []semanticplan.SemanticPlanNode{producer, attribution},
		Output:    semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{group}},
	}
}

func TestResolvedMetricAttributionRequestFailsClosed(t *testing.T) {
	t.Run("multiple_dimensions_are_independent", func(t *testing.T) {
		request := additiveAttributionRequestForTest()
		request.Dimensions = []string{"country", "channel"}
		if err := attribution.ValidateResolvedMetricAttributionRequest(request); err != nil {
			t.Fatalf("bounded multi-dimension attribution request did not validate: %v", err)
		}
	})

	t.Run("duplicate_dimensions", func(t *testing.T) {
		request := additiveAttributionRequestForTest()
		request.Dimensions = []string{"country", "country"}
		if err := attribution.ValidateResolvedMetricAttributionRequest(request); err == nil {
			t.Fatal("duplicate attribution dimensions unexpectedly validated")
		}
	})

	t.Run("dimension_bound", func(t *testing.T) {
		request := additiveAttributionRequestForTest()
		request.Dimensions = make([]string, attribution.MaxMetricAttributionDimensions+1)
		for i := range request.Dimensions {
			request.Dimensions[i] = fmt.Sprintf("dimension_%d", i)
		}
		if err := attribution.ValidateResolvedMetricAttributionRequest(request); err == nil {
			t.Fatal("unbounded attribution dimensions unexpectedly validated")
		}
	})

	t.Run("invalid_period", func(t *testing.T) {
		request := additiveAttributionRequestForTest()
		request.Current.End = request.Current.Start
		if err := attribution.ValidateResolvedMetricAttributionRequest(request); err == nil {
			t.Fatal("empty current period unexpectedly validated")
		}
	})

	t.Run("time_dimension_as_breakdown", func(t *testing.T) {
		request := additiveAttributionRequestForTest()
		request.Dimensions = []string{request.TimeDimensionRef}
		if err := attribution.ValidateResolvedMetricAttributionRequest(request); err == nil {
			t.Fatal("time dimension unexpectedly admitted as attribution breakdown")
		}
	})
}

func TestBuildAdditiveAttributionNodeSelectsOneIndependentRequestedDimension(t *testing.T) {
	request := additiveAttributionRequestForTest()
	request.Dimensions = []string{"country", "channel"}
	group := semanticplan.GroupBy{Name: "channel", Dataset: "orders"}
	node, err := attribution.BuildAdditiveAttributionNode(
		"revenue__attribution__channel",
		request,
		additiveAttributionPlanForTest(),
		semanticplan.SemanticPlanNodeInput{NodeID: "revenue", Grain: []semanticplan.GroupBy{group}},
		group,
		additiveAttributionTimeDimensionForTest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if node.DimensionRef != "channel" || len(node.Base.OutputGrain) != 1 {
		t.Fatalf("independent attribution node = %#v", node)
	}
}

func TestBuildAdditiveAttributionNodeRequiresCanonicalAdditiveProof(t *testing.T) {
	request := additiveAttributionRequestForTest()
	group := semanticplan.GroupBy{Name: "country", Dataset: "orders"}
	input := semanticplan.SemanticPlanNodeInput{NodeID: "revenue", Grain: []semanticplan.GroupBy{group}}
	timeDimension := additiveAttributionTimeDimensionForTest()

	ratio := attribution.MetricAttributionPlan{
		Metric:    "revenue",
		Exactness: attribution.MetricAttributionExact,
		Strategy:  semanticplan.MetricAttributionRatioMixRate,
		Components: []attribution.MetricAttributionComponent{
			{Metric: "revenue", Role: attribution.MetricAttributionNumerator},
			{Metric: "sessions", Role: attribution.MetricAttributionDenominator},
		},
		Reconciliation: semanticplan.MetricAttributionReconcileMixRate,
	}
	if _, err := attribution.BuildAdditiveAttributionNode("attribution", request, ratio, input, group, timeDimension); err == nil {
		t.Fatal("ratio attribution plan unexpectedly constructed an additive node")
	}

	unsupported := attribution.MetricAttributionPlan{
		Metric:    "revenue",
		Exactness: attribution.MetricAttributionUnsupported,
		Reason:    attribution.MetricAttributionReasonUnsupportedDecomposition,
	}
	if _, err := attribution.BuildAdditiveAttributionNode("attribution", request, unsupported, input, group, timeDimension); err == nil {
		t.Fatal("unsupported attribution plan unexpectedly constructed an additive node")
	}
}

func TestBuildAdditiveAttributionNodeRequiresResolvedTimeEvidence(t *testing.T) {
	request := additiveAttributionRequestForTest()
	group := semanticplan.GroupBy{Name: "country", Dataset: "orders"}
	input := semanticplan.SemanticPlanNodeInput{NodeID: "revenue", Grain: []semanticplan.GroupBy{group}}

	wrongRef := additiveAttributionTimeDimensionForTest()
	wrongRef.Name = "created_at"
	if _, err := attribution.BuildAdditiveAttributionNode("attribution", request, additiveAttributionPlanForTest(), input, group, wrongRef); err == nil {
		t.Fatal("mismatched time-dimension evidence unexpectedly constructed an attribution node")
	}

	unresolved := additiveAttributionTimeDimensionForTest()
	unresolved.Expression = expression.ResolvedExpression{}
	if _, err := attribution.BuildAdditiveAttributionNode("attribution", request, additiveAttributionPlanForTest(), input, group, unresolved); err == nil {
		t.Fatal("unresolved time-dimension evidence unexpectedly constructed an attribution node")
	}
}

func TestAdditiveAttributionNodeClosesSemanticDAGContract(t *testing.T) {
	plan := additiveAttributionSemanticPlanForTest(t)
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatalf("validate semantic DAG: %v", err)
	}

	cloned, err := semanticplan.CloneNodes(plan.Nodes)
	if err != nil {
		t.Fatalf("clone semantic nodes: %v", err)
	}
	original := plan.Nodes[1].(semanticplan.AdditiveAttributionNode)
	copyNode := cloned[1].(semanticplan.AdditiveAttributionNode)
	original.Base.Dimensions[0] = "mutated"
	original.MetricState.Metrics[0] = "mutated"
	if copyNode.Base.Dimensions[0] != "country" || copyNode.MetricState.Metrics[0] != "revenue" {
		t.Fatalf("cloned attribution node aliases owned slices: %#v", copyNode)
	}

	// The ownership assertion above intentionally mutates the original plan's
	// backing slices. Explain must be checked against a fresh valid graph rather
	// than the deliberately corrupted ownership-test fixture.
	plan = additiveAttributionSemanticPlanForTest(t)
	explanation, err := semanticplan.Explain(plan)
	if err != nil {
		t.Fatalf("explain semantic DAG: %v", err)
	}
	if len(explanation.Nodes) != 2 || explanation.Nodes[1].Attribution == nil {
		t.Fatalf("attribution explanation missing: %#v", explanation.Nodes)
	}
	evidence := explanation.Nodes[1].Attribution
	if evidence.MetricRef != "revenue" || evidence.DimensionRef != "country" || evidence.Decomposition != "additive" || evidence.Strategy != semanticplan.MetricAttributionAdditiveContribution || evidence.Reconciliation != semanticplan.MetricAttributionReconcileSegmentDelta {
		t.Fatalf("attribution explanation = %#v", evidence)
	}
}

func TestMetricProjectionPruningRetainsAdditiveAttributionRoot(t *testing.T) {
	plan := additiveAttributionSemanticPlanForTest(t)
	changed, err := (optimizer.MetricProjectionPruningRule{}).Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	if changed || len(plan.Nodes) != 2 || plan.Nodes[1].Kind() != semanticplan.SemanticPlanNodeAdditiveAttribution {
		t.Fatalf("additive attribution pruning changed=%v nodes=%#v", changed, plan.Nodes)
	}
}

func TestAdditiveAttributionFingerprintOwnsPeriodIdentity(t *testing.T) {
	plan := additiveAttributionSemanticPlanForTest(t)
	before, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Nodes[1].(semanticplan.AdditiveAttributionNode)
	node.Current.End = node.Current.End.AddDate(0, 1, 0)
	plan.Nodes[1] = node
	after, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("attribution fingerprint ignored current-period semantics")
	}
}

func TestAdditiveAttributionFingerprintOwnsTimeDimensionEvidence(t *testing.T) {
	plan := additiveAttributionSemanticPlanForTest(t)
	before, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Nodes[1].(semanticplan.AdditiveAttributionNode)
	node.TimeDimension.Expression.Source = "orders.created_at"
	plan.Nodes[1] = node
	after, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("attribution fingerprint ignored resolved time-dimension expression")
	}
}

func TestAdditiveAttributionFingerprintCanonicalizesEquivalentInstants(t *testing.T) {
	plan := additiveAttributionSemanticPlanForTest(t)
	utcFingerprint, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Nodes[1].(semanticplan.AdditiveAttributionNode)
	jst := time.FixedZone("JST", 9*60*60)
	node.Baseline.Start = node.Baseline.Start.In(jst)
	node.Baseline.End = node.Baseline.End.In(jst)
	node.Current.Start = node.Current.Start.In(jst)
	node.Current.End = node.Current.End.In(jst)
	plan.Nodes[1] = node
	jstFingerprint, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if utcFingerprint != jstFingerprint {
		t.Fatalf("equivalent attribution instants produced different fingerprints: %q != %q", utcFingerprint, jstFingerprint)
	}
}
