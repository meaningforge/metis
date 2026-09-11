package builder

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

const sourceSelectionNodeID = "source_selection"

// BuildSourceSelection constructs the typed node for a metric-free dimension
// or distinct-values query after its join tree is final.
func BuildSourceSelection(plan *semanticplan.SemanticPlan, q *resolver.SemanticQuerySpec) (semanticplan.SourceSelectionNode, bool) {
	if plan == nil || q == nil || len(q.Dimensions) == 0 {
		return semanticplan.SourceSelectionNode{}, false
	}
	base := semanticplan.SemanticPlanNodeBase{
		ID:                        sourceSelectionNodeID,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceSelection,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceSelection),
		OutputGrain:               append([]semanticplan.GroupBy(nil), plan.Groups...),
	}
	for _, dimension := range q.Dimensions {
		base.Dimensions = append(base.Dimensions, dimension.Name)
	}
	source := semanticplan.SemanticSourceState{
		SourceRoots:      []string{plan.Root.Name},
		RequiredDatasets: sourceSelectionRequiredDatasets(plan),
		Root:             plan.Root,
		Joins:            append([]semanticplan.Join(nil), plan.Joins...),
	}
	for i := range plan.Predicates {
		predicate := plan.Predicates[i]
		base.Predicates = append(base.Predicates, semanticplan.SemanticPlanNodePredicate{
			Scope:       semanticplan.SemanticPredicateSourceRead,
			OwnerNodeID: base.ID,
			Proof:       semanticplan.SemanticPredicateProofDatasetReachability,
			Predicate:   &predicate,
		})
	}
	node := semanticplan.SourceSelectionNode{Base: base, Source: source, Mode: SourceSelectionMode(q.Intent)}
	if err := semanticplan.ValidateNode(node); err != nil {
		return semanticplan.SourceSelectionNode{}, false
	}
	return node, true
}

// SourceSelectionMode converts resolved query intent into its closed typed
// source-selection semantic state.
func SourceSelectionMode(intent query.QueryIntent) semanticplan.SourceSelectionMode {
	if intent == query.QueryIntentDistinctValues {
		return semanticplan.SourceSelectionDistinctValues
	}
	return semanticplan.SourceSelectionGroupedValues
}

func sourceSelectionRequiredDatasets(plan *semanticplan.SemanticPlan) []string {
	seen := map[string]bool{plan.Root.Name: true}
	required := []string{plan.Root.Name}
	for _, join := range plan.Joins {
		for _, dataset := range []string{join.FromDataset, join.ToDataset} {
			if dataset != "" && !seen[dataset] {
				seen[dataset] = true
				required = append(required, dataset)
			}
		}
	}
	return required
}
