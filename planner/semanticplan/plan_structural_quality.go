package semanticplan

import "fmt"

// StructuralQualityVector is a deterministic internal safety signal for
// duplicated semantic-plan work. It is not an estimate of database runtime
// cost and must not be used as a physical optimizer objective.
type StructuralQualityVector struct {
	SourceGroups    int
	EvaluationNodes int
	EvaluationJoins int
	Joins           int
}

// StructuralQuality returns the component-wise work vector used by optimizer
// regression tests. Lower is structurally no worse for these proven dimensions.
func StructuralQuality(plan *SemanticPlan) StructuralQualityVector {
	summary := SummarizeSemanticPlan(plan)
	return StructuralQualityVector{
		SourceGroups:    summary.SourceGroups,
		EvaluationNodes: summary.EvaluationNodes,
		EvaluationJoins: summary.EvaluationJoins,
		Joins:           summary.Joins,
	}
}

// ValidateStructuralNonRegression rejects accidental growth in the protected
// duplicated-work dimensions. Semantic-plan validation remains authoritative;
// this check is only an additional regression signal after a semantics-proven
// rewrite.
func ValidateStructuralNonRegression(before, after *SemanticPlan) error {
	beforeQuality := StructuralQuality(before)
	afterQuality := StructuralQuality(after)
	if afterQuality.SourceGroups > beforeQuality.SourceGroups {
		return fmt.Errorf("source groups increased from %d to %d", beforeQuality.SourceGroups, afterQuality.SourceGroups)
	}
	if afterQuality.EvaluationNodes > beforeQuality.EvaluationNodes {
		return fmt.Errorf("evaluation nodes increased from %d to %d", beforeQuality.EvaluationNodes, afterQuality.EvaluationNodes)
	}
	if afterQuality.EvaluationJoins > beforeQuality.EvaluationJoins {
		return fmt.Errorf("evaluation joins increased from %d to %d", beforeQuality.EvaluationJoins, afterQuality.EvaluationJoins)
	}
	if afterQuality.Joins > beforeQuality.Joins {
		return fmt.Errorf("joins increased from %d to %d", beforeQuality.Joins, afterQuality.Joins)
	}
	return nil
}
