package planner_test

import (
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
)

type semanticNodeFixture struct {
	Node                      semanticplan.SemanticPlanNode
	ID                        string
	Kind                      semanticplan.SemanticPlanNodeKind
	Boundary                  semanticplan.SemanticPlanNodeBoundary
	PredicateBoundaryEvidence semanticplan.SemanticPredicateBoundaryEvidence
	Inputs                    []semanticNodeInputFixture
	Metrics                   []string
	Dimensions                []string
	OutputGrain               []semanticplan.GroupBy
	SourceRoots               []string
	RequiredDatasets          []string
	Root                      semanticplan.DatasetRef
	Joins                     []semanticplan.Join
	Predicates                []semanticNodePredicateFixture
	ShareGroup                string
	SharedGrainEvidence       *semanticplan.MetricSharedGrainEvidence
	ExtensionEvidence         []extension.Evidence
}

type semanticNodeInputFixture struct {
	NodeID string
	Grain  []semanticplan.GroupBy
}

type semanticNodePredicateFixture struct {
	Scope       semanticplan.SemanticPredicateScope
	OwnerNodeID string
	Proof       semanticplan.SemanticPredicatePlacementProof
	Predicate   *semanticplan.Predicate
	Post        *semanticplan.PostEvaluationPredicate
}

// semanticPlanForTest is the external-test counterpart of the planner package's
// structural fixture helper. Production SemanticPlan values store typed nodes
// only; this helper never recreates a legacy evaluation envelope.
func semanticPlanForTest(plan semanticplan.SemanticPlan, stages []semanticNodeFixture) *semanticplan.SemanticPlan {
	plan.Nodes = make([]semanticplan.SemanticPlanNode, 0, len(stages))
	for _, stage := range stages {
		plan.Nodes = append(plan.Nodes, semanticPlanNodeForTest(stage))
	}
	return &plan
}

func replaceSemanticPlanNodeFixturesForTest(plan *semanticplan.SemanticPlan, stages []semanticNodeFixture) {
	converted := semanticPlanForTest(*plan, stages)
	plan.Nodes = converted.Nodes
}

func semanticPlanNodeForTest(stage semanticNodeFixture) semanticplan.SemanticPlanNode {
	node := stage.Node
	if node == nil {
		node = canonicalNodeForTest(stage.Kind)
	}
	if stage.Kind == "" && node != nil {
		stage.Kind = semanticplan.SemanticPlanNodeKind(node.Kind())
	}
	if stage.Boundary == "" {
		switch stage.Kind {
		case semanticplan.SemanticPlanNodeSourceAggregate:
			stage.Boundary = semanticplan.SemanticPlanNodeBoundarySourceAggregate
		case semanticplan.SemanticPlanNodePostAggregate:
			stage.Boundary = semanticplan.SemanticPlanNodeBoundaryPostAggregate
		case semanticplan.SemanticPlanNodeJoinAggregates, semanticplan.SemanticPlanNodeCrossJoinAggregates:
			stage.Boundary = semanticplan.SemanticPlanNodeBoundaryAggregateComposition
		case semanticplan.SemanticPlanNodeCumulativeWindow, semanticplan.SemanticPlanNodeTimeOffset, semanticplan.SemanticPlanNodeOffsetToGrain, semanticplan.SemanticPlanNodeConversion, semanticplan.SemanticPlanNodeSemiAdditiveLast, semanticplan.SemanticPlanNodeSemiAdditiveFirst:
			stage.Boundary = semanticplan.SemanticPlanNodeBoundarySemanticIsolation
		case semanticplan.SemanticPlanNodeSourceSelection:
			stage.Boundary = semanticplan.SemanticPlanNodeBoundarySourceSelection
		}
	}
	base := semanticplan.SemanticPlanNodeBase{
		ID:                        stage.ID,
		Boundary:                  semanticplan.SemanticPlanNodeBoundary(stage.Boundary),
		PredicateBoundaryEvidence: stage.PredicateBoundaryEvidence,
		Dimensions:                append([]string(nil), stage.Dimensions...),
		OutputGrain:               append([]semanticplan.GroupBy(nil), stage.OutputGrain...),
		ExtensionEvidence:         append([]extension.Evidence(nil), stage.ExtensionEvidence...),
	}
	for _, input := range stage.Inputs {
		base.Inputs = append(base.Inputs, semanticplan.SemanticPlanNodeInput{NodeID: input.NodeID, Grain: append([]semanticplan.GroupBy(nil), input.Grain...)})
	}
	for _, predicate := range stage.Predicates {
		base.Predicates = append(base.Predicates, semanticplan.SemanticPlanNodePredicate{
			Scope:       predicate.Scope,
			OwnerNodeID: predicate.OwnerNodeID,
			Proof:       predicate.Proof,
			Predicate:   predicate.Predicate,
			Post:        predicate.Post,
		})
	}
	source := semanticplan.SemanticSourceState{
		SourceRoots:      append([]string(nil), stage.SourceRoots...),
		RequiredDatasets: append([]string(nil), stage.RequiredDatasets...),
		Root:             stage.Root,
		Joins:            append([]semanticplan.Join(nil), stage.Joins...),
	}
	metric := semanticplan.SemanticMetricState{
		Metrics:             append([]string(nil), stage.Metrics...),
		ShareGroup:          stage.ShareGroup,
		SharedGrainEvidence: stage.SharedGrainEvidence,
	}

	switch value := node.(type) {
	case semanticplan.SourceAggregateNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.PostAggregateNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.JoinAggregatesNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.CrossJoinAggregatesNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.CumulativeWindowNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.TimeOffsetNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.OffsetToGrainNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.ConversionNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.SemiAdditiveNode:
		value.Base, value.Source, value.MetricState = base, source, metric
		return value
	case semanticplan.SourceSelectionNode:
		value.Base, value.Source = base, source
		return value
	default:
		return nil
	}
}

func canonicalNodeForTest(kind semanticplan.SemanticPlanNodeKind) semanticplan.SemanticPlanNode {
	switch kind {
	case semanticplan.SemanticPlanNodeSourceAggregate, "":
		return semanticplan.SourceAggregateNode{}
	case semanticplan.SemanticPlanNodePostAggregate:
		return semanticplan.PostAggregateNode{}
	case semanticplan.SemanticPlanNodeJoinAggregates:
		return semanticplan.JoinAggregatesNode{}
	case semanticplan.SemanticPlanNodeCrossJoinAggregates:
		return semanticplan.CrossJoinAggregatesNode{}
	case semanticplan.SemanticPlanNodeCumulativeWindow:
		return semanticplan.CumulativeWindowNode{}
	case semanticplan.SemanticPlanNodeTimeOffset:
		return semanticplan.TimeOffsetNode{}
	case semanticplan.SemanticPlanNodeOffsetToGrain:
		return semanticplan.OffsetToGrainNode{}
	case semanticplan.SemanticPlanNodeConversion:
		return semanticplan.ConversionNode{}
	case semanticplan.SemanticPlanNodeSemiAdditiveLast:
		return semanticplan.SemiAdditiveNode{Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "last"}}
	case semanticplan.SemanticPlanNodeSemiAdditiveFirst:
		return semanticplan.SemiAdditiveNode{Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "first"}}
	case semanticplan.SemanticPlanNodeSourceSelection:
		return semanticplan.SourceSelectionNode{Mode: semanticplan.SourceSelectionGroupedValues}
	default:
		return nil
	}
}

func semanticPlanNodeFixturesForTest(value any) []semanticNodeFixture {
	var plan *semanticplan.SemanticPlan
	switch value := value.(type) {
	case *semanticplan.SemanticPlan:
		plan = value
	case semanticplan.SemanticPlan:
		plan = &value
	case nil:
		return nil
	default:
		panic("semanticPlanNodeFixturesForTest requires semanticplan.SemanticPlan")
	}
	stages := make([]semanticNodeFixture, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		stages = append(stages, semanticPlanStageForTest(node))
	}
	return stages
}

func semanticPlanStageForTest(node semanticplan.SemanticPlanNode) semanticNodeFixture {
	base := node.NodeBase()
	stage := semanticNodeFixture{
		Node:                      node,
		ID:                        base.ID,
		Kind:                      semanticplan.SemanticPlanNodeKind(node.Kind()),
		Boundary:                  semanticplan.SemanticPlanNodeBoundary(base.Boundary),
		PredicateBoundaryEvidence: base.PredicateBoundaryEvidence,
		Dimensions:                append([]string(nil), base.Dimensions...),
		OutputGrain:               append([]semanticplan.GroupBy(nil), base.OutputGrain...),
		ExtensionEvidence:         append([]extension.Evidence(nil), base.ExtensionEvidence...),
	}
	for _, input := range base.Inputs {
		stage.Inputs = append(stage.Inputs, semanticNodeInputFixture{NodeID: input.NodeID, Grain: append([]semanticplan.GroupBy(nil), input.Grain...)})
	}
	for _, predicate := range base.Predicates {
		stage.Predicates = append(stage.Predicates, semanticNodePredicateFixture{
			Scope:       predicate.Scope,
			OwnerNodeID: predicate.OwnerNodeID,
			Proof:       predicate.Proof,
			Predicate:   predicate.Predicate,
			Post:        predicate.Post,
		})
	}
	setSource := func(source semanticplan.SemanticSourceState, metric semanticplan.SemanticMetricState) {
		stage.SourceRoots = append([]string(nil), source.SourceRoots...)
		stage.RequiredDatasets = append([]string(nil), source.RequiredDatasets...)
		stage.Root = source.Root
		stage.Joins = append([]semanticplan.Join(nil), source.Joins...)
		stage.Metrics = append([]string(nil), metric.Metrics...)
		stage.ShareGroup = metric.ShareGroup
		stage.SharedGrainEvidence = metric.SharedGrainEvidence
	}
	switch value := node.(type) {
	case semanticplan.SourceAggregateNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.PostAggregateNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.JoinAggregatesNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.CrossJoinAggregatesNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.CumulativeWindowNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.TimeOffsetNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.OffsetToGrainNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.ConversionNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.SemiAdditiveNode:
		setSource(value.Source, value.MetricState)
	case semanticplan.SourceSelectionNode:
		setSource(value.Source, semanticplan.SemanticMetricState{})
	}
	return stage
}
