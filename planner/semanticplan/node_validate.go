package semanticplan

import (
	"fmt"

	"github.com/meaningforge/metis/serrors"
)

func NodeBoundaryForKind(kind SemanticPlanNodeKind) SemanticPlanNodeBoundary {
	switch kind {
	case SemanticPlanNodeSourceAggregate:
		return SemanticPlanNodeBoundarySourceAggregate
	case SemanticPlanNodePostAggregate:
		return SemanticPlanNodeBoundaryPostAggregate
	case SemanticPlanNodeJoinAggregates, SemanticPlanNodeCrossJoinAggregates:
		return SemanticPlanNodeBoundaryAggregateComposition
	case SemanticPlanNodeCumulativeWindow, SemanticPlanNodeTimeOffset, SemanticPlanNodeOffsetToGrain, SemanticPlanNodeConversion, SemanticPlanNodeSemiAdditiveLast, SemanticPlanNodeSemiAdditiveFirst, SemanticPlanNodeAdditiveAttribution, SemanticPlanNodeRatioAttribution:
		return SemanticPlanNodeBoundarySemanticIsolation
	case SemanticPlanNodeSourceSelection:
		return SemanticPlanNodeBoundarySourceSelection
	default:
		return ""
	}
}

func PredicateBoundaryEvidence(boundary SemanticPlanNodeBoundary) SemanticPredicateBoundaryEvidence {
	switch boundary {
	case SemanticPlanNodeBoundarySourceAggregate, SemanticPlanNodeBoundarySourceSelection:
		return SemanticPredicateBoundaryEvidence{
			Movement: SemanticPredicateBoundaryAllowedWithProof,
			Proof:    SemanticPredicateProofDatasetReachability,
		}
	case SemanticPlanNodeBoundaryPostAggregate, SemanticPlanNodeBoundaryAggregateComposition, SemanticPlanNodeBoundarySemanticIsolation:
		return SemanticPredicateBoundaryEvidence{
			Movement: SemanticPredicateBoundaryBlocked,
			Proof:    SemanticPredicateProofNodeSemantics,
		}
	default:
		return SemanticPredicateBoundaryEvidence{}
	}
}

func ValidateSemanticPlanDAG(plan *SemanticPlan) error {
	if plan == nil {
		return nil
	}
	if err := validateSemanticNodeDAG(plan.Nodes, plan.Output.Grain, plan.Output.Predicates); err != nil {
		return err
	}
	shareGroups := make(map[string]string)
	for _, node := range plan.Nodes {
		state, ok := NodeMetricState(node)
		if !ok || state.ShareGroup == "" {
			continue
		}
		if node.Kind() != SemanticPlanNodeSourceAggregate {
			return serrors.Internal("semantic share group contains a non-source node", map[string]any{"share_group": state.ShareGroup, "node": node.NodeBase().ID})
		}
		work, err := SourceScanWorkOfNode(node)
		if err != nil {
			return fmt.Errorf("semantic source share group %q node %q: %w", state.ShareGroup, node.NodeBase().ID, err)
		}
		identity, err := work.Identity()
		if err != nil {
			return err
		}
		if previous, ok := shareGroups[state.ShareGroup]; ok && previous != identity {
			return serrors.Internal("semantic source share group contains an incompatible node", map[string]any{"share_group": state.ShareGroup, "node": node.NodeBase().ID})
		}
		shareGroups[state.ShareGroup] = identity
	}
	return nil
}

func validateSemanticNodeDAG(nodes []SemanticPlanNode, outputGrain []GroupBy, post []PostEvaluationPredicate) error {
	byID := make(map[string]SemanticPlanNode, len(nodes))
	attributionNodes := 0
	for index, node := range nodes {
		if err := ValidateNode(node); err != nil {
			return fmt.Errorf("semantic node at index %d: %w", index, err)
		}
		base := node.NodeBase()
		switch node.(type) {
		case AdditiveAttributionNode, RatioAttributionNode:
			attributionNodes++
			if attributionNodes > 1 {
				return fmt.Errorf("semantic plan must contain exactly one independent attribution node")
			}
		}
		if base.ID == "" {
			return serrors.Internal("semantic node has an empty id", map[string]any{"index": index})
		}
		if _, exists := byID[base.ID]; exists {
			return serrors.Internal("semantic node id is duplicated", map[string]any{"node": base.ID})
		}
		if base.Boundary == "" || base.Boundary != NodeBoundaryForKind(node.Kind()) {
			return serrors.Internal("semantic node has an unsupported or mismatched evaluation boundary", map[string]any{"node": base.ID, "kind": string(node.Kind()), "boundary": string(base.Boundary)})
		}
		if err := validateSemanticNodePredicateBoundary(node); err != nil {
			return err
		}
		for _, input := range base.Inputs {
			producer, ok := byID[input.NodeID]
			if !ok {
				return serrors.Internal("semantic node has a missing or unordered dependency", map[string]any{"node": base.ID, "input": input.NodeID})
			}
			if canonicalGrainKey(input.Grain) != canonicalGrainKey(producer.NodeBase().OutputGrain) {
				return serrors.Internal("semantic node input grain does not match its producer grain", map[string]any{"node": base.ID, "input": input.NodeID, "input_grain": canonicalGrainKey(input.Grain), "producer_grain": canonicalGrainKey(producer.NodeBase().OutputGrain)})
			}
		}
		if err := validateSemanticNodeCompositionBoundary(node); err != nil {
			return err
		}
		for _, predicate := range base.Predicates {
			if err := validateSemanticNodePredicate(base.ID, predicate); err != nil {
				return err
			}
		}
		if metricState, ok := NodeMetricState(node); ok {
			if node.Kind() == SemanticPlanNodeSourceAggregate {
				source, _ := NodeSourceState(node)
				if err := validateSourcePopulationPreservationEvidence(base.ID, source); err != nil {
					return err
				}
			}
			if metricState.SharedGrainEvidence != nil {
				evidence := metricState.SharedGrainEvidence
				if !evidence.FanoutSafe {
					return serrors.Internal("semantic node carries fanout-unsafe shared-grain evidence", map[string]any{"node": base.ID})
				}
				if evidence.GrainKey != "" && evidence.GrainKey != canonicalGrainKey(base.OutputGrain) {
					return serrors.Internal("semantic node shared-grain evidence does not match its output grain", map[string]any{"node": base.ID, "evidence_grain": evidence.GrainKey, "output_grain": canonicalGrainKey(base.OutputGrain)})
				}
			}
		}
		byID[base.ID] = node
	}
	outputNames := make(map[string]struct{}, len(byID)+len(outputGrain))
	for id := range byID {
		outputNames[id] = struct{}{}
	}
	for _, group := range outputGrain {
		if group.Name != "" {
			outputNames[group.Name] = struct{}{}
		}
	}
	for _, predicate := range post {
		if predicate.Name == "" {
			return serrors.Internal("post-evaluation predicate output name is required", nil)
		}
		if _, ok := outputNames[predicate.Name]; !ok {
			return serrors.Internal("post-evaluation predicate references a missing output", map[string]any{"output": predicate.Name})
		}
	}
	return nil
}

func validateSourcePopulationPreservationEvidence(nodeID string, source SemanticSourceState) error {
	if len(source.PopulationPreservationEvidence) != len(source.Joins) {
		return serrors.Internal("semantic source node population-preservation evidence does not cover its joins", map[string]any{"node": nodeID, "joins": len(source.Joins), "evidence": len(source.PopulationPreservationEvidence)})
	}
	for i, evidence := range source.PopulationPreservationEvidence {
		if err := ValidatePopulationPreservationEvidence(evidence); err != nil {
			return fmt.Errorf("semantic source node %q population-preservation evidence: %w", nodeID, err)
		}
		join := source.Joins[i]
		relationship := ""
		if join.Relationship != nil {
			relationship = join.Relationship.Name
		}
		if evidence.Relationship+"\x00"+evidence.FromDataset+"\x00"+evidence.ToDataset != relationship+"\x00"+join.FromDataset+"\x00"+join.ToDataset {
			return serrors.Internal("semantic source node population-preservation evidence does not match its join", map[string]any{"node": nodeID, "index": i})
		}
	}
	return nil
}

func validateSemanticNodePredicateBoundary(node SemanticPlanNode) error {
	base := node.NodeBase()
	want := PredicateBoundaryEvidence(base.Boundary)
	if base.PredicateBoundaryEvidence.Movement == "" || base.PredicateBoundaryEvidence.Proof == "" || base.PredicateBoundaryEvidence != want {
		return serrors.Internal("semantic node predicate boundary evidence does not match its boundary", map[string]any{"node": base.ID, "boundary": string(base.Boundary)})
	}
	return nil
}

func validateSemanticNodePredicate(nodeID string, predicate SemanticPlanNodePredicate) error {
	if !validSemanticPredicateScope(predicate.Scope) || predicate.Proof == "" {
		return serrors.Internal("semantic node predicate has invalid placement metadata", map[string]any{"node": nodeID, "scope": string(predicate.Scope)})
	}
	if (predicate.Predicate == nil) == (predicate.Post == nil) {
		return serrors.Internal("semantic predicate scope must carry exactly one representation", map[string]any{"scope": string(predicate.Scope)})
	}
	if predicate.Scope == SemanticPredicateFinalOutput {
		return serrors.Internal("semantic node cannot own a final-output predicate", map[string]any{"node": nodeID})
	}
	if predicate.OwnerNodeID != nodeID {
		return serrors.Internal("semantic predicate is owned by a different node", map[string]any{"node": nodeID, "owner_node": predicate.OwnerNodeID})
	}
	return nil
}

func validateSemanticNodeCompositionBoundary(node SemanticPlanNode) error {
	base := node.NodeBase()
	switch node.Kind() {
	case SemanticPlanNodePostAggregate, SemanticPlanNodeJoinAggregates:
		outputGrain := canonicalGrainKey(base.OutputGrain)
		for _, input := range base.Inputs {
			if canonicalGrainKey(input.Grain) != outputGrain {
				return serrors.Internal("semantic node requires a compatible aggregate grain", map[string]any{"node": base.ID, "want_grain": outputGrain, "input": input.NodeID, "input_grain": canonicalGrainKey(input.Grain)})
			}
		}
	case SemanticPlanNodeCrossJoinAggregates:
		if canonicalGrainKey(base.OutputGrain) != "scalar" {
			return serrors.Internal("semantic cross-join node must produce a scalar grain", map[string]any{"node": base.ID})
		}
		for _, input := range base.Inputs {
			if canonicalGrainKey(input.Grain) != "scalar" {
				return serrors.Internal("semantic cross-join node requires a scalar aggregate input", map[string]any{"node": base.ID, "input": input.NodeID})
			}
		}
	}
	return nil
}

func validSemanticPredicateScope(scope SemanticPredicateScope) bool {
	switch scope {
	case SemanticPredicateSourceRead,
		SemanticPredicatePreAggregation,
		SemanticPredicatePostAggregation,
		SemanticPredicateFinalOutput:
		return true
	default:
		return false
	}
}
