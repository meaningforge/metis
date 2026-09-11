package builder

import (
	"github.com/meaningforge/metis/planner/semanticplan"
)

// validateConversionQueryGrains proves that every visible conversion output
// grouping is owned by the base-event population. Candidate matching may read
// conversion-event fields, but those fields must not redefine the denominator
// grain after assignment.
func validateConversionQueryGrains(nodes []semanticplan.SemanticPlanNode, groups []semanticplan.GroupBy) error {
	return ValidateConversionQueryGrains(nodes, groups)
}
