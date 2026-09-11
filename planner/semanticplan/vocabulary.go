package semanticplan

import "github.com/meaningforge/metis/query"

// PostEvaluationPredicate constrains the relation that materializes a metric
// result, rather than a source relation below an aggregation boundary.
type PostEvaluationPredicate struct {
	Name   string
	Filter query.Filter
}

type SemanticPredicateScope string

const (
	SemanticPredicateSourceRead      SemanticPredicateScope = "source_read"
	SemanticPredicatePreAggregation  SemanticPredicateScope = "pre_aggregation"
	SemanticPredicatePostAggregation SemanticPredicateScope = "post_aggregation"
	SemanticPredicateFinalOutput     SemanticPredicateScope = "final_output"
)

type SemanticPredicatePlacementProof string

const (
	SemanticPredicateProofSourceOwnership     SemanticPredicatePlacementProof = "source_ownership"
	SemanticPredicateProofDatasetReachability SemanticPredicatePlacementProof = "dataset_reachability"
	SemanticPredicateProofNodeSemantics       SemanticPredicatePlacementProof = "node_semantics"
	SemanticPredicateProofOutputSemantics     SemanticPredicatePlacementProof = "output_semantics"
)

type SemanticPredicateBoundaryMovement string

const (
	SemanticPredicateBoundaryAllowedWithProof SemanticPredicateBoundaryMovement = "allowed_with_proof"
	SemanticPredicateBoundaryBlocked          SemanticPredicateBoundaryMovement = "blocked"
)

// SemanticPredicateBoundaryEvidence records semantic proof for moving a
// predicate toward a node input. It is not a cost-based pushdown preference.
type SemanticPredicateBoundaryEvidence struct {
	Movement SemanticPredicateBoundaryMovement
	Proof    SemanticPredicatePlacementProof
}

// SourceSelectionMode distinguishes the two metric-free selection semantics.
type SourceSelectionMode string

const (
	SourceSelectionGroupedValues  SourceSelectionMode = "grouped_values"
	SourceSelectionDistinctValues SourceSelectionMode = "distinct_values"
)

// OptimizationStep is planner-owned trace evidence stored on a SemanticPlan.
// It does not affect semantic-plan meaning or the Agent-facing contract.
type OptimizationStep struct {
	Rule    string
	Changed bool
}
