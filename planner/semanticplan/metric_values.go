package semanticplan

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

// RollupComponent is one retained source-aggregate state value. Column is the
// plan-owned physical name carried across a rollup boundary; Expression is the
// source-grain aggregate that produces it; Merge combines finer-grain values.
type RollupComponent struct {
	Name       string
	Column     string
	Expression expression.ResolvedExpression
	Merge      string
}

// RollupContract records whether a source metric retains enough aggregate
// state to be re-aggregated at a coarser grain. Components is deliberately a
// sequence: algebraic aggregates such as AVG require more than one state value.
type RollupContract struct {
	Function   string
	Algebra    expression.RollupAlgebra
	Components []RollupComponent
	Finalize   expression.FinalizeKind
	Mergeable  bool
	Reason     string
}

// ValidateRollupContract keeps retained state closed and reproducible before
// physical lowering. An unmergeable zero-value contract is valid; positive
// mergeability requires complete component and finalization evidence.
func ValidateRollupContract(contract RollupContract) error {
	if !contract.Mergeable {
		if len(contract.Components) != 0 {
			return fmt.Errorf("unmergeable rollup contract retains components")
		}
		return nil
	}
	if strings.TrimSpace(contract.Function) == "" || contract.Algebra == "" {
		return fmt.Errorf("mergeable rollup contract requires function and algebra")
	}
	if len(contract.Components) == 0 {
		return fmt.Errorf("mergeable rollup contract requires retained components")
	}
	seenNames, seenColumns := map[string]struct{}{}, map[string]struct{}{}
	for _, component := range contract.Components {
		if strings.TrimSpace(component.Name) == "" || strings.TrimSpace(component.Column) == "" || strings.TrimSpace(component.Merge) == "" {
			return fmt.Errorf("rollup component is incomplete")
		}
		if !component.Expression.IsResolved() {
			return fmt.Errorf("rollup component %q has unresolved expression", component.Name)
		}
		if _, exists := seenNames[component.Name]; exists {
			return fmt.Errorf("rollup component name %q is duplicated", component.Name)
		}
		if _, exists := seenColumns[component.Column]; exists {
			return fmt.Errorf("rollup component column %q is duplicated", component.Column)
		}
		seenNames[component.Name] = struct{}{}
		seenColumns[component.Column] = struct{}{}
	}
	switch contract.Finalize {
	case expression.FinalizeIdentity:
		if len(contract.Components) != 1 {
			return fmt.Errorf("identity rollup finalization requires one component")
		}
	case expression.FinalizeRatio:
		if len(contract.Components) != 2 {
			return fmt.Errorf("ratio rollup finalization requires two components")
		}
		if _, ok := seenNames[expression.PartialStateSum]; !ok {
			return fmt.Errorf("ratio rollup finalization requires sum state")
		}
		if _, ok := seenNames[expression.PartialStateCount]; !ok {
			return fmt.Errorf("ratio rollup finalization requires count state")
		}
	default:
		return fmt.Errorf("mergeable rollup contract has unsupported finalization %q", contract.Finalize)
	}
	return nil
}

// OffsetToGrainPlan is the engine-neutral boundary proof used by physical
// lowering. Custom calendars carry the declared bucket and ordinal relation.
type OffsetToGrainPlan struct {
	TimeDimension      string
	QueryGrain         query.TimeGrain
	BoundaryGrain      query.TimeGrain
	CustomCalendar     bool
	Dataset            DatasetRef
	BoundaryBucket     *ossie.Field
	BoundaryExpression string
	QueryOrdinal       *ossie.Field
	OrdinalExpression  string
}
