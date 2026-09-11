package builder

import (
	"errors"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func normalizeFixtureStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func semanticNodeFixtureBoundaryForKind(kind semanticplan.SemanticPlanNodeKind) semanticplan.SemanticPlanNodeBoundary {
	return semanticplan.NodeBoundaryForKind(kind)
}

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

func clonesemanticNodeFixtureValue(stage semanticNodeFixture) semanticNodeFixture {
	owned := stage
	owned.Inputs = nil
	for _, input := range stage.Inputs {
		owned.Inputs = append(owned.Inputs, semanticNodeInputFixture{NodeID: input.NodeID, Grain: cloneGroups(input.Grain)})
	}
	owned.Metrics = append([]string(nil), stage.Metrics...)
	owned.Dimensions = append([]string(nil), stage.Dimensions...)
	owned.OutputGrain = cloneGroups(stage.OutputGrain)
	owned.SourceRoots = append([]string(nil), stage.SourceRoots...)
	owned.RequiredDatasets = append([]string(nil), stage.RequiredDatasets...)
	owned.Joins = append([]semanticplan.Join(nil), stage.Joins...)
	owned.Predicates = clonesemanticNodePredicateFixtures(stage.Predicates)
	owned.ExtensionEvidence = append([]extension.Evidence(nil), stage.ExtensionEvidence...)
	if stage.SharedGrainEvidence != nil {
		evidence := semanticplan.CloneMetricSharedGrainEvidence(*stage.SharedGrainEvidence)
		owned.SharedGrainEvidence = &evidence
	}
	return owned
}

func semanticNodeFixturesFromPlanNodes(nodes []semanticplan.SemanticPlanNode) ([]semanticNodeFixture, error) {
	out := make([]semanticNodeFixture, 0, len(nodes))
	for _, node := range nodes {
		stage, err := semanticNodeFixtureFromPlanNode(node)
		if err != nil {
			return nil, err
		}
		out = append(out, stage)
	}
	return out, nil
}

func semanticNodeFixtureFromPlanNode(node semanticplan.SemanticPlanNode) (semanticNodeFixture, error) {
	owned, err := semanticplan.CloneNodes([]semanticplan.SemanticPlanNode{node})
	if err != nil || len(owned) != 1 {
		return semanticNodeFixture{}, err
	}
	node = owned[0]
	base := node.NodeBase()
	stage := semanticNodeFixture{Node: node, ID: base.ID, Kind: node.Kind(), Boundary: base.Boundary, PredicateBoundaryEvidence: base.PredicateBoundaryEvidence, Dimensions: append([]string(nil), base.Dimensions...), OutputGrain: cloneGroups(base.OutputGrain), ExtensionEvidence: append([]extension.Evidence(nil), base.ExtensionEvidence...)}
	for _, input := range base.Inputs {
		stage.Inputs = append(stage.Inputs, semanticNodeInputFixture{NodeID: input.NodeID, Grain: cloneGroups(input.Grain)})
	}
	for _, predicate := range base.Predicates {
		stage.Predicates = append(stage.Predicates, semanticNodePredicateFixture{Scope: predicate.Scope, OwnerNodeID: predicate.OwnerNodeID, Proof: predicate.Proof, Predicate: predicate.Predicate, Post: predicate.Post})
	}
	if source, ok := semanticplan.NodeSourceState(node); ok {
		stage.SourceRoots = append([]string(nil), source.SourceRoots...)
		stage.RequiredDatasets = append([]string(nil), source.RequiredDatasets...)
		stage.Root = source.Root
		stage.Joins = append([]semanticplan.Join(nil), source.Joins...)
	}
	if metric, ok := semanticplan.NodeMetricState(node); ok {
		stage.Metrics = append([]string(nil), metric.Metrics...)
		stage.ShareGroup = metric.ShareGroup
		if metric.SharedGrainEvidence != nil {
			evidence := semanticplan.CloneMetricSharedGrainEvidence(*metric.SharedGrainEvidence)
			stage.SharedGrainEvidence = &evidence
		}
	}
	return stage, nil
}

func shadowsemanticNodeFixtureFromPlanNode(node semanticplan.SemanticPlanNode) (semanticNodeFixture, error) {
	return semanticNodeFixtureFromPlanNode(node)
}

func sourceScanWorkOf(stage semanticNodeFixture) (semanticplan.SourceScanWork, error) {
	if stage.Kind != semanticplan.SemanticPlanNodeSourceAggregate {
		return semanticplan.SourceScanWork{}, fmt.Errorf("source scan work requires %q stage, got %q", semanticplan.SemanticPlanNodeSourceAggregate, stage.Kind)
	}
	predicates := make([]semanticplan.Predicate, 0, len(stage.Predicates))
	for _, predicate := range stage.Predicates {
		if predicate.Scope != semanticplan.SemanticPredicatePreAggregation || predicate.Predicate == nil {
			return semanticplan.SourceScanWork{}, fmt.Errorf("%w: stage %q has an unsharable predicate", semanticplan.ErrUnsharableSourceScan, stage.ID)
		}
		predicates = append(predicates, *predicate.Predicate)
	}
	sourceRoots := normalizeFixtureStringSet(stage.SourceRoots)
	requiredDatasets := normalizeFixtureStringSet(stage.RequiredDatasets)
	return semanticplan.SourceScanWork{Root: stage.Root, SourceRoots: sourceRoots, RequiredDatasets: requiredDatasets, Joins: stage.Joins, OutputGrain: stage.OutputGrain, Predicates: predicates}, nil
}

func sourceEvaluationSubplanIdentity(stage semanticNodeFixture) (string, error) {
	work, err := sourceScanWorkOf(stage)
	if err != nil {
		return "", err
	}
	return work.Identity()
}

func sharableSourceScanIdentity(stage semanticNodeFixture) (string, bool, error) {
	identity, err := sourceEvaluationSubplanIdentity(stage)
	if errors.Is(err, semanticplan.ErrUnsharableSourceScan) {
		return "", false, nil
	}
	return identity, err == nil, err
}

func validateSourceSelectionStage(stage semanticNodeFixture) error {
	if stage.Kind != semanticplan.SemanticPlanNodeSourceSelection {
		return nil
	}
	node, ok := stage.Node.(semanticplan.SourceSelectionNode)
	if !ok {
		return fmt.Errorf("semantic stage %q source selection has typed node %T", stage.ID, stage.Node)
	}
	switch node.Mode {
	case semanticplan.SourceSelectionGroupedValues, semanticplan.SourceSelectionDistinctValues:
	case "":
		return fmt.Errorf("semantic stage %q source selection has no mode", stage.ID)
	default:
		return fmt.Errorf("semantic stage %q has unsupported source-selection mode %q", stage.ID, node.Mode)
	}
	if len(stage.Metrics) != 0 {
		return fmt.Errorf("semantic stage %q evaluates metrics", stage.ID)
	}
	if stage.ShareGroup != "" {
		return fmt.Errorf("semantic stage %q joins a share group", stage.ID)
	}
	return nil
}

func clonesemanticNodeFixtures(stages []semanticNodeFixture) []semanticNodeFixture {
	out := make([]semanticNodeFixture, 0, len(stages))
	for _, stage := range stages {
		out = append(out, clonesemanticNodeFixtureValue(stage))
	}
	return out
}

func clonesemanticNodePredicateFixtures(predicates []semanticNodePredicateFixture) []semanticNodePredicateFixture {
	out := make([]semanticNodePredicateFixture, 0, len(predicates))
	for _, predicate := range predicates {
		owned := predicate
		if predicate.Predicate != nil {
			value := *predicate.Predicate
			owned.Predicate = &value
		}
		if predicate.Post != nil {
			value := *predicate.Post
			owned.Post = &value
		}
		out = append(out, owned)
	}
	return out
}

func shadowSemanticPlanNode(stage semanticNodeFixture) (semanticplan.SemanticPlanNode, error) {
	if stage.Node == nil {
		return nil, fmt.Errorf("semantic stage %q has no typed node", stage.ID)
	}
	if err := semanticplan.ValidateNode(stage.Node); err != nil {
		return nil, fmt.Errorf("semantic stage %q typed node: %w", stage.ID, err)
	}
	node, err := semanticPlanNodeWithStageStructureForTest(stage.Node, stage)
	if err != nil {
		return nil, err
	}
	if err := semanticplan.ValidateNode(node); err != nil {
		return nil, fmt.Errorf("semantic stage %q typed node: %w", stage.ID, err)
	}
	return node, nil
}

func semanticPlanNodeWithStageStructureForTest(node semanticplan.SemanticPlanNode, stage semanticNodeFixture) (semanticplan.SemanticPlanNode, error) {
	if node == nil {
		return nil, fmt.Errorf("semantic plan node is required")
	}
	if node.NodeBase().ID != stage.ID {
		return nil, fmt.Errorf("semantic plan node %q does not match structural stage %q", node.NodeBase().ID, stage.ID)
	}
	if node.Kind() != semanticplan.SemanticPlanNodeKind(stage.Kind) {
		return nil, fmt.Errorf("semantic plan node %q kind %q does not match structural stage kind %q", stage.ID, node.Kind(), stage.Kind)
	}

	base := shadowSemanticPlanNodeBaseForTest(stage)
	source := shadowSemanticSourceStateForTest(stage)
	metricState := shadowSemanticMetricStateForTest(stage)
	updated, err := semanticplan.WithNodeBase(node, base)
	if err != nil {
		return nil, err
	}
	updated, err = semanticplan.WithNodeSourceState(updated, source)
	if err != nil {
		return nil, err
	}
	if _, ok := updated.(semanticplan.SourceSelectionNode); ok {
		return updated, nil
	}
	return semanticplan.WithNodeMetricState(updated, metricState)
}

func shadowSemanticPlanNodeBaseForTest(stage semanticNodeFixture) semanticplan.SemanticPlanNodeBase {
	var inputs []semanticplan.SemanticPlanNodeInput
	if len(stage.Inputs) > 0 {
		inputs = make([]semanticplan.SemanticPlanNodeInput, 0, len(stage.Inputs))
		for _, input := range stage.Inputs {
			inputs = append(inputs, semanticplan.SemanticPlanNodeInput{NodeID: input.NodeID, Grain: append([]semanticplan.GroupBy(nil), input.Grain...)})
		}
	}
	var predicates []semanticplan.SemanticPlanNodePredicate
	if len(stage.Predicates) > 0 {
		predicates = make([]semanticplan.SemanticPlanNodePredicate, 0, len(stage.Predicates))
		for _, predicate := range stage.Predicates {
			predicates = append(predicates, semanticplan.SemanticPlanNodePredicate{Scope: predicate.Scope, OwnerNodeID: predicate.OwnerNodeID, Proof: predicate.Proof, Predicate: predicate.Predicate, Post: predicate.Post})
		}
	}
	return semanticplan.SemanticPlanNodeBase{
		ID: stage.ID, Boundary: semanticplan.SemanticPlanNodeBoundary(stage.Boundary), PredicateBoundaryEvidence: stage.PredicateBoundaryEvidence,
		Inputs: inputs, Dimensions: append([]string(nil), stage.Dimensions...), OutputGrain: append([]semanticplan.GroupBy(nil), stage.OutputGrain...),
		Predicates: predicates, ExtensionEvidence: append([]extension.Evidence(nil), stage.ExtensionEvidence...),
	}
}

func shadowSemanticSourceStateForTest(stage semanticNodeFixture) semanticplan.SemanticSourceState {
	return semanticplan.SemanticSourceState{SourceRoots: append([]string(nil), stage.SourceRoots...), RequiredDatasets: append([]string(nil), stage.RequiredDatasets...), Root: stage.Root, Joins: append([]semanticplan.Join(nil), stage.Joins...)}
}

func shadowSemanticMetricStateForTest(stage semanticNodeFixture) semanticplan.SemanticMetricState {
	var shared *semanticplan.MetricSharedGrainEvidence
	if stage.SharedGrainEvidence != nil {
		value := semanticplan.CloneMetricSharedGrainEvidence(*stage.SharedGrainEvidence)
		shared = &value
	}
	return semanticplan.SemanticMetricState{Metrics: append([]string(nil), stage.Metrics...), ShareGroup: stage.ShareGroup, SharedGrainEvidence: shared}
}

// semanticPlanForTest keeps stage-shaped fixtures concise while production plan
// storage remains typed-node authoritative. New tests should prefer typed node
// literals directly; older structural fixtures are converted only inside this
// test helper.
func semanticPlanForTest(plan semanticplan.SemanticPlan, stages []semanticNodeFixture) *semanticplan.SemanticPlan {
	stages = clonesemanticNodeFixtures(stages)
	nodes := make([]semanticplan.SemanticPlanNode, 0, len(stages))
	for i := range stages {
		if stages[i].Kind == "" {
			stages[i].Kind = semanticplan.SemanticPlanNodeSourceAggregate
		}
		if stages[i].Node == nil {
			stages[i].Node = canonicalNodeFor(stages[i].Kind)
		}
		// Historical stage-shaped test fixtures often omitted the typed node ID
		// because the stage ID used to be the only stored identity. Preserve that
		// fixture convenience only inside this test adapter: an omitted node ID is
		// canonicalized from the structural stage ID, while a non-empty mismatch
		// still fails closed in shadowSemanticPlanNode.
		if stages[i].Node.NodeBase().ID == "" {
			base := stages[i].Node.NodeBase()
			base.ID = stages[i].ID
			node, err := semanticplan.WithNodeBase(stages[i].Node, base)
			if err != nil {
				panic(err)
			}
			stages[i].Node = node
		}
		node, err := shadowSemanticPlanNode(stages[i])
		if err != nil {
			panic(err)
		}
		nodes = append(nodes, node)
	}
	plan.Nodes = nodes
	return &plan
}

func semanticPlanNodeFixturesForTest(plan *semanticplan.SemanticPlan) []semanticNodeFixture {
	if plan == nil {
		return nil
	}
	stages, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		panic(err)
	}
	return stages
}

func replaceSemanticPlanNodeFixturesForTest(plan *semanticplan.SemanticPlan, stages []semanticNodeFixture) {
	converted := semanticPlanForTest(*plan, stages)
	plan.Nodes = converted.Nodes
}

func mutateSemanticPlanNodeFixturesForTest(plan *semanticplan.SemanticPlan, mutate func([]semanticNodeFixture)) {
	stages := semanticPlanNodeFixturesForTest(plan)
	mutate(stages)
	replaceSemanticPlanNodeFixturesForTest(plan, stages)
}
