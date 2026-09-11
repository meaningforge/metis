package builder

import (
	"testing"
	"time"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func ratioAttributionRequestForTest() attribution.ResolvedMetricAttributionRequest {
	request := additiveAttributionRequestForTest()
	request.MetricRef = "conversion_rate"
	return request
}

func ratioAttributionPlanForTest() attribution.MetricAttributionPlan {
	return attribution.MetricAttributionPlan{
		Metric:    "conversion_rate",
		Exactness: attribution.MetricAttributionExact,
		Strategy:  semanticplan.MetricAttributionRatioMixRate,
		Components: []attribution.MetricAttributionComponent{
			{Metric: "converted", Role: attribution.MetricAttributionNumerator},
			{Metric: "sessions", Role: attribution.MetricAttributionDenominator},
		},
		Reconciliation: semanticplan.MetricAttributionReconcileMixRate,
	}
}

func buildRatioAttributionNodeForTest(t *testing.T) (semanticplan.GroupBy, semanticplan.RatioAttributionNode) {
	t.Helper()
	group := semanticplan.GroupBy{Name: "country", Dataset: "events"}
	node, err := attribution.BuildRatioAttributionNode(
		"ratio_attribution",
		ratioAttributionRequestForTest(),
		ratioAttributionPlanForTest(),
		semanticplan.SemanticPlanNodeInput{NodeID: "converted", Grain: []semanticplan.GroupBy{group}},
		semanticplan.SemanticPlanNodeInput{NodeID: "sessions", Grain: []semanticplan.GroupBy{group}},
		group,
		semanticplan.GroupBy{
			Name:       "order_time",
			Dataset:    "events",
			Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "events.order_time"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return group, node
}

func ratioAttributionSemanticPlanForTest(t *testing.T) *semanticplan.SemanticPlan {
	t.Helper()
	group, attribution := buildRatioAttributionNodeForTest(t)
	producer := func(metric string) semanticplan.SourceAggregateNode {
		return semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:                        metric,
				Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
				Dimensions:                []string{"country"},
				OutputGrain:               []semanticplan.GroupBy{group},
			},
			MetricState: semanticplan.SemanticMetricState{Metrics: []string{metric}},
		}
	}
	return &semanticplan.SemanticPlan{
		Requested: []string{"conversion_rate"},
		Nodes: []semanticplan.SemanticPlanNode{
			producer("converted"),
			producer("sessions"),
			attribution,
		},
		Output: semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{group}},
	}
}

func TestBuildRatioAttributionNodeOwnsOrderedOperandsAndUndefinedPolicy(t *testing.T) {
	_, node := buildRatioAttributionNodeForTest(t)
	if node.MetricRef != "conversion_rate" || node.NumeratorRef != "converted" || node.DenominatorRef != "sessions" {
		t.Fatalf("ratio attribution refs = %#v", node)
	}
	if len(node.Base.Inputs) != 2 || node.Base.Inputs[0].NodeID != "converted" || node.Base.Inputs[1].NodeID != "sessions" {
		t.Fatalf("ratio attribution inputs = %#v", node.Base.Inputs)
	}
	if got := semanticplan.RatioAttributionUndefinedNullWithDefinedFlag; got != semanticplan.RatioAttributionUndefinedNullWithDefinedFlag {
		t.Fatalf("undefined ratio policy = %q", got)
	}
	if got := semanticplan.RatioAttributionFullUnionEntryExit; got != semanticplan.RatioAttributionFullUnionEntryExit {
		t.Fatalf("population alignment = %q", got)
	}
}

func TestBuildRatioAttributionNodeRequiresCanonicalRatioProof(t *testing.T) {
	request := ratioAttributionRequestForTest()
	group := semanticplan.GroupBy{Name: "country", Dataset: "events"}
	numerator := semanticplan.SemanticPlanNodeInput{NodeID: "converted", Grain: []semanticplan.GroupBy{group}}
	denominator := semanticplan.SemanticPlanNodeInput{NodeID: "sessions", Grain: []semanticplan.GroupBy{group}}
	timeDimension := semanticplan.GroupBy{
		Name:       "order_time",
		Dataset:    "events",
		Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "events.order_time"},
	}

	additive := additiveAttributionPlanForTest()
	additive.Metric = request.MetricRef
	additive.Components[0].Metric = request.MetricRef
	if _, err := attribution.BuildRatioAttributionNode("ratio", request, additive, numerator, denominator, group, timeDimension); err == nil {
		t.Fatal("additive proof unexpectedly constructed a ratio attribution node")
	}
	if _, err := attribution.BuildRatioAttributionNode("ratio", request, ratioAttributionPlanForTest(), denominator, numerator, group, timeDimension); err == nil {
		t.Fatal("reversed ratio operands unexpectedly constructed a ratio attribution node")
	}
	targetAsNumerator := ratioAttributionPlanForTest()
	targetAsNumerator.Components[0].Metric = request.MetricRef
	targetInput := numerator
	targetInput.NodeID = request.MetricRef
	if _, err := attribution.BuildRatioAttributionNode("ratio", request, targetAsNumerator, targetInput, denominator, group, timeDimension); err == nil {
		t.Fatal("ratio target reused as an operand unexpectedly constructed a node")
	}

	wrongGrain := denominator
	wrongGrain.Grain = []semanticplan.GroupBy{{Name: "channel", Dataset: "events"}}
	if _, err := attribution.BuildRatioAttributionNode("ratio", request, ratioAttributionPlanForTest(), numerator, wrongGrain, group, timeDimension); err == nil {
		t.Fatal("incompatible ratio operand grain unexpectedly constructed a node")
	}
}

func TestBuildRatioAttributionNodeRequiresResolvedTimeEvidence(t *testing.T) {
	request := ratioAttributionRequestForTest()
	group := semanticplan.GroupBy{Name: "country", Dataset: "events"}
	numerator := semanticplan.SemanticPlanNodeInput{NodeID: "converted", Grain: []semanticplan.GroupBy{group}}
	denominator := semanticplan.SemanticPlanNodeInput{NodeID: "sessions", Grain: []semanticplan.GroupBy{group}}

	unresolved := semanticplan.GroupBy{Name: "order_time", Dataset: "events"}
	if _, err := attribution.BuildRatioAttributionNode("ratio", request, ratioAttributionPlanForTest(), numerator, denominator, group, unresolved); err == nil {
		t.Fatal("unresolved ratio time evidence unexpectedly constructed a node")
	}
}

func TestRatioAttributionNodeClosesSemanticDAGContract(t *testing.T) {
	plan := ratioAttributionSemanticPlanForTest(t)
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatalf("validate semantic DAG: %v", err)
	}

	cloned, err := semanticplan.CloneNodes(plan.Nodes)
	if err != nil {
		t.Fatalf("clone semantic nodes: %v", err)
	}
	original := plan.Nodes[2].(semanticplan.RatioAttributionNode)
	copyNode := cloned[2].(semanticplan.RatioAttributionNode)
	original.Base.Inputs[0].NodeID = "mutated"
	original.Base.Dimensions[0] = "mutated"
	original.MetricState.Metrics[0] = "mutated"
	if copyNode.Base.Inputs[0].NodeID != "converted" || copyNode.Base.Dimensions[0] != "country" || copyNode.MetricState.Metrics[0] != "conversion_rate" {
		t.Fatalf("cloned ratio attribution node aliases owned slices: %#v", copyNode)
	}

	explanation, err := semanticplan.Explain(ratioAttributionSemanticPlanForTest(t))
	if err != nil {
		t.Fatalf("explain semantic DAG: %v", err)
	}
	if len(explanation.Nodes) != 3 || explanation.Nodes[2].Attribution == nil {
		t.Fatalf("ratio attribution explanation missing: %#v", explanation.Nodes)
	}
	evidence := explanation.Nodes[2].Attribution
	if evidence.MetricRef != "conversion_rate" || evidence.NumeratorRef != "converted" || evidence.DenominatorRef != "sessions" || evidence.Decomposition != "ratio" || evidence.Strategy != semanticplan.MetricAttributionRatioMixRate || evidence.Reconciliation != semanticplan.MetricAttributionReconcileMixRate || evidence.UndefinedRatioPolicy != semanticplan.RatioAttributionUndefinedNullWithDefinedFlag || evidence.PopulationAlignment != semanticplan.RatioAttributionFullUnionEntryExit {
		t.Fatalf("ratio attribution explanation = %#v", evidence)
	}
}

func TestMetricProjectionPruningRetainsRatioAttributionRootAndOperands(t *testing.T) {
	plan := ratioAttributionSemanticPlanForTest(t)
	changed, err := (optimizer.MetricProjectionPruningRule{}).Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	if changed || len(plan.Nodes) != 3 {
		t.Fatalf("ratio attribution pruning changed=%v nodes=%#v", changed, plan.Nodes)
	}
	if plan.Nodes[2].Kind() != semanticplan.SemanticPlanNodeRatioAttribution {
		t.Fatalf("ratio attribution root was not retained: %#v", plan.Nodes)
	}
}

func TestRatioAttributionFingerprintOwnsOperandIdentity(t *testing.T) {
	plan := ratioAttributionSemanticPlanForTest(t)
	before, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}

	producer := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	producer.Base.ID = "qualified_converted"
	producer.MetricState.Metrics[0] = "qualified_converted"
	plan.Nodes[0] = producer
	node := plan.Nodes[2].(semanticplan.RatioAttributionNode)
	node.NumeratorRef = "qualified_converted"
	node.Base.Inputs[0].NodeID = "qualified_converted"
	plan.Nodes[2] = node
	after, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("ratio attribution fingerprint ignored numerator identity")
	}
}

func TestRatioAttributionFingerprintOwnsPeriodAndCanonicalizesInstants(t *testing.T) {
	plan := ratioAttributionSemanticPlanForTest(t)
	before, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)

	}
	node := plan.Nodes[2].(semanticplan.RatioAttributionNode)
	jst := time.FixedZone("JST", 9*60*60)
	node.Baseline.Start = node.Baseline.Start.In(jst)
	node.Baseline.End = node.Baseline.End.In(jst)
	node.Current.Start = node.Current.Start.In(jst)
	node.Current.End = node.Current.End.In(jst)
	plan.Nodes[2] = node
	equivalent, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if before != equivalent {
		t.Fatalf("equivalent ratio attribution instants changed fingerprint: %q != %q", before, equivalent)
	}

	node.Current.End = node.Current.End.AddDate(0, 1, 0)
	plan.Nodes[2] = node
	changed, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if before == changed {
		t.Fatal("ratio attribution fingerprint ignored current-period semantics")
	}
}

func TestRatioAttributionFingerprintOwnsTimeDimensionEvidence(t *testing.T) {
	plan := ratioAttributionSemanticPlanForTest(t)
	before, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	node := plan.Nodes[2].(semanticplan.RatioAttributionNode)
	node.TimeDimension.Expression.Source = "events.created_at"
	plan.Nodes[2] = node
	after, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("ratio attribution fingerprint ignored resolved time-dimension expression")
	}
}

func TestRatioAttributionNodeRejectsIndistinguishableOperands(t *testing.T) {
	_, node := buildRatioAttributionNodeForTest(t)
	node.DenominatorRef = node.NumeratorRef
	node.Base.Inputs[1].NodeID = node.NumeratorRef
	if err := semanticplan.ValidateNode(node); err == nil {
		t.Fatal("ratio attribution node with indistinguishable operands unexpectedly validated")
	}
}

func TestRatioAttributionNodeRejectsSharedFilterOwnership(t *testing.T) {
	_, node := buildRatioAttributionNodeForTest(t)
	predicate := semanticplan.Predicate{}
	node.Base.Predicates = []semanticplan.SemanticPlanNodePredicate{{
		Scope:       semanticplan.SemanticPredicatePreAggregation,
		OwnerNodeID: node.Base.ID,
		Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
		Predicate:   &predicate,
	}}
	if err := semanticplan.ValidateNode(node); err == nil {
		t.Fatal("ratio attribution node unexpectedly owned a shared source filter")
	}
}
