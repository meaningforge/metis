package semanticplan

import "github.com/meaningforge/metis/serrors"

// validateSemanticPlanDAGContract is the canonical SemanticPlan graph contract.
// It composes structural graph validation with planner-owned metadata and
// reachability invariants so downstream optimization, explain, and lowering can
// consume one validated semantic IR without re-deriving semantic assumptions.
//
// Every condition here is unreachable from any query or semantic model: the
// graph under inspection is one the planner just built. Failures are therefore
// reported through serrors.Internal, and carry the offending identity as
// structured details rather than only inside a formatted message.
func ValidateDAGContract(plan *SemanticPlan) error {
	if err := validateCanonicalStringSet("requested output", "", plan.Requested); err != nil {
		return err
	}

	for _, node := range plan.Nodes {
		if err := ValidateNode(node); err != nil {
			return serrors.Internal("semantic plan has an invalid typed node", map[string]any{
				"error": err.Error(),
			})
		}
		base := node.NodeBase()
		wantBoundary := NodeBoundaryForKind(node.Kind())
		if wantBoundary == "" {
			return serrors.Internal("semantic node has an unsupported evaluation kind", map[string]any{
				"node": base.ID,
				"kind": string(node.Kind()),
			})
		}
		if base.Boundary != wantBoundary {
			return serrors.Internal("semantic node boundary does not match its evaluation kind", map[string]any{
				"node":          base.ID,
				"kind":          string(node.Kind()),
				"boundary":      string(base.Boundary),
				"want_boundary": string(wantBoundary),
			})
		}
		if metricState, ok := NodeMetricState(node); ok {
			if err := validateCanonicalStringSet("metrics", base.ID, metricState.Metrics); err != nil {
				return err
			}
		}
		if err := validateCanonicalStringSet("dimensions", base.ID, base.Dimensions); err != nil {
			return err
		}
		if source, ok := NodeSourceState(node); ok {
			if err := validateCanonicalStringSet("source roots", base.ID, source.SourceRoots); err != nil {
				return err
			}
			if err := validateCanonicalStringSet("required datasets", base.ID, source.RequiredDatasets); err != nil {
				return err
			}
		}

		inputs := make(map[string]struct{}, len(base.Inputs))
		for _, input := range base.Inputs {
			if input.NodeID == "" {
				return serrors.Internal("semantic node has an input with an empty node id", map[string]any{
					"node": base.ID,
				})
			}
			if _, exists := inputs[input.NodeID]; exists {
				return serrors.Internal("semantic node has a duplicate input", map[string]any{
					"node":  base.ID,
					"input": input.NodeID,
				})
			}
			inputs[input.NodeID] = struct{}{}
		}
	}

	if err := ValidateSemanticPlanDAG(plan); err != nil {
		return err
	}
	if err := validateNodeDatasetReachability(plan.Nodes); err != nil {
		return err
	}
	return nil
}

// validateCanonicalStringSet rejects empty and duplicate members of a canonical
// set. scope names which set is being checked and node identifies its owner
// when the set belongs to one; both are reported as details so a caller does
// not have to parse them back out of the message.
func validateCanonicalStringSet(scope string, node string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return serrors.Internal("canonical semantic set contains an empty value", canonicalSetDetails(scope, node, ""))
		}
		if _, exists := seen[value]; exists {
			return serrors.Internal("canonical semantic set contains a duplicate value", canonicalSetDetails(scope, node, value))
		}
		seen[value] = struct{}{}
	}
	return nil
}

func canonicalSetDetails(scope string, node string, value string) map[string]any {
	details := map[string]any{"scope": scope}
	if node != "" {
		details["node"] = node
	}
	if value != "" {
		details["value"] = value
	}
	return details
}
