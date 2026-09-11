package sqlplan

import (
	"fmt"
	"math"
	"strings"

	"github.com/meaningforge/metis/query"
)

func Validate(plan *Plan) error {
	return validate(plan, "")
}

// ValidateForRenderer validates structural SQLPlan invariants and proves that
// every opaque expression is compatible with the selected Renderer dialect.
func ValidateForRenderer(plan *Plan, expressionDialect string) error {
	expressionDialect = strings.TrimSpace(expressionDialect)
	if expressionDialect == "" {
		return fmt.Errorf("selected Renderer expression dialect is required")
	}
	return validate(plan, expressionDialect)
}

func validate(plan *Plan, expressionDialect string) error {
	if plan == nil {
		return fmt.Errorf("SQL plan is required")
	}
	if plan.Root == "" {
		return fmt.Errorf("SQL plan root is required")
	}

	positions := make(map[QueryBlockID]int, len(plan.Blocks))
	for i := range plan.Blocks {
		block := &plan.Blocks[i]
		if block.ID == "" {
			return fmt.Errorf("query block at position %d has an empty ID", i)
		}
		if _, duplicate := positions[block.ID]; duplicate {
			return fmt.Errorf("duplicate query block ID %q", block.ID)
		}
		positions[block.ID] = i
	}
	rootPosition, ok := positions[plan.Root]
	if !ok {
		return fmt.Errorf("SQL plan root %q does not exist", plan.Root)
	}

	for i := range plan.Blocks {
		if err := validateBlock(&plan.Blocks[i], i, positions, expressionDialect); err != nil {
			return fmt.Errorf("query block %q: %w", plan.Blocks[i].ID, err)
		}
	}

	reachable := map[QueryBlockID]bool{}
	var visit func(QueryBlockID)
	visit = func(id QueryBlockID) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		block := plan.Blocks[positions[id]]
		for _, input := range block.Inputs {
			visit(input.Block)
		}
	}
	visit(plan.Blocks[rootPosition].ID)
	for _, block := range plan.Blocks {
		if !reachable[block.ID] {
			return fmt.Errorf("query block %q is unreachable from root %q", block.ID, plan.Root)
		}
	}
	return nil
}

func validateBlock(block *QueryBlock, position int, positions map[QueryBlockID]int, rendererDialect string) error {
	inputs := make(map[string]QueryInput, len(block.Inputs))
	for _, input := range block.Inputs {
		if strings.TrimSpace(input.Alias) == "" || input.Block == "" {
			return fmt.Errorf("input alias and block are required")
		}
		if _, duplicate := inputs[input.Alias]; duplicate {
			return fmt.Errorf("duplicate input alias %q", input.Alias)
		}
		dependencyPosition, ok := positions[input.Block]
		if !ok {
			return fmt.Errorf("input %q references missing block %q", input.Alias, input.Block)
		}
		if dependencyPosition >= position {
			return fmt.Errorf("input %q references block %q that is not topologically earlier", input.Alias, input.Block)
		}
		if input.Mode != QueryInputCTE && input.Mode != QueryInputDerivedTable {
			return fmt.Errorf("input %q has unsupported mode %q", input.Alias, input.Mode)
		}
		inputs[input.Alias] = input
	}

	aliases := map[string]bool{}
	if err := validateRelation(block.From, inputs, aliases); err != nil {
		return fmt.Errorf("from relation: %w", err)
	}
	for i, join := range block.Joins {
		if join.Kind != JoinInner && join.Kind != JoinFullOuter && join.Kind != JoinCross {
			return fmt.Errorf("join %d has unsupported kind %q", i, join.Kind)
		}
		if err := validateRelation(join.Relation, inputs, aliases); err != nil {
			return fmt.Errorf("join %d relation: %w", i, err)
		}
		if join.Kind == JoinCross {
			if join.On != nil {
				return fmt.Errorf("cross join %d must not have a predicate", i)
			}
		} else if err := validateExpr(join.On, aliases, rendererDialect); err != nil {
			return fmt.Errorf("join %d predicate: %w", i, err)
		}
	}

	if len(block.Projections) == 0 {
		return fmt.Errorf("at least one projection is required")
	}
	projectionAliases := map[string]bool{}
	for i, projection := range block.Projections {
		if strings.TrimSpace(projection.Alias) == "" {
			return fmt.Errorf("projection %d has an empty alias", i)
		}
		if projectionAliases[projection.Alias] {
			return fmt.Errorf("duplicate projection alias %q", projection.Alias)
		}
		projectionAliases[projection.Alias] = true
		if err := validateExpr(projection.Expr, aliases, rendererDialect); err != nil {
			return fmt.Errorf("projection %q: %w", projection.Alias, err)
		}
	}
	for i, predicate := range block.Predicates {
		if err := validateExpr(predicate.Left, aliases, rendererDialect); err != nil {
			return fmt.Errorf("predicate %d: %w", i, err)
		}
		if err := validatePredicateCardinality(predicate.Operator, len(predicate.Values)); err != nil {
			return fmt.Errorf("predicate %d: %w", i, err)
		}
	}
	for i, expr := range block.GroupBy {
		if err := validateExpr(expr, aliases, rendererDialect); err != nil {
			return fmt.Errorf("grouping expression %d: %w", i, err)
		}
	}
	for i, order := range block.OrderBy {
		if order.Direction != query.SortAsc && order.Direction != query.SortDesc {
			return fmt.Errorf("ordering expression %d has unsupported direction %q", i, order.Direction)
		}
		if err := validateExpr(order.Expr, aliases, rendererDialect); err != nil {
			return fmt.Errorf("ordering expression %d: %w", i, err)
		}
	}
	if block.Limit != nil && *block.Limit < 0 {
		return fmt.Errorf("limit must not be negative")
	}
	return nil
}

func validateRelation(relation RelationRef, inputs map[string]QueryInput, aliases map[string]bool) error {
	kinds := 0
	if relation.Source != nil {
		kinds++
	}
	if relation.Input != nil {
		kinds++
	}
	if relation.FilteredSource != nil {
		kinds++
	}
	if kinds != 1 {
		return fmt.Errorf("relation must reference exactly one table source, filtered source or query input")
	}
	if strings.TrimSpace(relation.Alias) == "" {
		return fmt.Errorf("relation alias is required")
	}
	if aliases[relation.Alias] {
		return fmt.Errorf("duplicate relation alias %q", relation.Alias)
	}
	aliases[relation.Alias] = true
	if relation.Source != nil && strings.TrimSpace(relation.Source.Name) == "" {
		return fmt.Errorf("table source name is required")
	}
	if relation.Input != nil {
		if _, ok := inputs[relation.Input.Alias]; !ok {
			return fmt.Errorf("query input %q is not declared", relation.Input.Alias)
		}
	}
	if source := relation.FilteredSource; source != nil {
		if strings.TrimSpace(source.Name) == "" || len(source.Predicates) == 0 {
			return fmt.Errorf("filtered source requires a name and predicates")
		}
		for _, predicate := range source.Predicates {
			column, ok := predicate.Left.(ColumnRef)
			if !ok || column.Table != "" || strings.TrimSpace(column.Name) == "" {
				return fmt.Errorf("filtered source requires local physical columns")
			}
			if err := validatePredicateCardinality(predicate.Operator, len(predicate.Values)); err != nil {
				return err
			}
			for _, value := range predicate.Values {
				switch v := value.(type) {
				case string, bool, int64:
				case float64:
					if math.IsNaN(v) || math.IsInf(v, 0) {
						return fmt.Errorf("filtered source requires finite scalar values")
					}
				default:
					return fmt.Errorf("filtered source requires scalar values")
				}
			}
		}
	}
	return nil
}

func validatePredicateCardinality(operator query.FilterOperator, count int) error {
	switch operator {
	case query.FilterIsNull, query.FilterIsNotNull:
		if count != 0 {
			return fmt.Errorf("operator %q requires no values", operator)
		}
	case query.FilterIN, query.FilterNotIn:
		if count == 0 {
			return fmt.Errorf("operator %q requires at least one value", operator)
		}
	case query.FilterBetween:
		if count != 2 {
			return fmt.Errorf("operator %q requires exactly two values", operator)
		}
	case query.FilterEQ, query.FilterNEQ, query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE:
		if count != 1 {
			return fmt.Errorf("operator %q requires exactly one value", operator)
		}
	default:
		return fmt.Errorf("unsupported predicate operator %q", operator)
	}
	return nil
}

func validateExpr(expr Expr, aliases map[string]bool, rendererDialect string) error {
	if expr == nil {
		return fmt.Errorf("expression is required")
	}
	validateMany := func(expressions []Expr) error {
		for _, item := range expressions {
			if err := validateExpr(item, aliases, rendererDialect); err != nil {
				return err
			}
		}
		return nil
	}
	switch value := expr.(type) {
	case OpaqueExpr:
		if strings.TrimSpace(value.SQL) == "" {
			return fmt.Errorf("opaque expression SQL is required")
		}
		if strings.TrimSpace(value.Dialect) == "" {
			return fmt.Errorf("opaque expression dialect evidence is required")
		}
		if rendererDialect != "" && value.Dialect != rendererDialect && value.Dialect != ANSISQLExpressionDialect {
			return fmt.Errorf("opaque expression dialect %q is incompatible with Renderer dialect %q", value.Dialect, rendererDialect)
		}
	case ColumnRef:
		if strings.TrimSpace(value.Name) == "" {
			return fmt.Errorf("column name is required")
		}
		if value.Table != "" && !aliases[value.Table] {
			return fmt.Errorf("column %q references unavailable relation alias %q", value.Name, value.Table)
		}
	case BinaryExpr:
		if strings.TrimSpace(value.Operator) == "" {
			return fmt.Errorf("binary operator is required")
		}
		if err := validateMany([]Expr{value.Left, value.Right}); err != nil {
			return err
		}
	case NullOnZeroDivideExpr:
		return validateMany([]Expr{value.Numerator, value.Denominator})
	case LogicalExpr:
		if strings.TrimSpace(value.Operator) == "" || len(value.Terms) == 0 {
			return fmt.Errorf("logical operator and terms are required")
		}
		return validateMany(value.Terms)
	case FunctionCallExpr:
		if strings.TrimSpace(value.Name) == "" {
			return fmt.Errorf("function name is required")
		}
		return validateMany(value.Args)
	case NullTestExpr:
		return validateExpr(value.Expr, aliases, rendererDialect)
	case CaseExpr:
		if len(value.Branches) == 0 {
			return fmt.Errorf("case expression requires at least one branch")
		}
		for _, branch := range value.Branches {
			if err := validateMany([]Expr{branch.When, branch.Then}); err != nil {
				return err
			}
		}
		if value.Else == nil {
			return fmt.Errorf("case expression requires an else expression")
		}
		return validateExpr(value.Else, aliases, rendererDialect)
	case ParenthesizedExpr:
		return validateExpr(value.Expr, aliases, rendererDialect)
	case CastExpr:
		if value.Type != CastDecimal20Scale12 && value.Type != CastDecimal38Scale18 {
			return fmt.Errorf("unsupported cast type %q", value.Type)
		}
		return validateExpr(value.Expr, aliases, rendererDialect)
	case TimeGrainExpr:
		if value.Grain == "" {
			return fmt.Errorf("time grain is required")
		}
		return validateExpr(value.Expr, aliases, rendererDialect)
	case CalendarShiftExpr:
		if value.Unit == "" {
			return fmt.Errorf("calendar shift unit is required")
		}
		return validateExpr(value.Expr, aliases, rendererDialect)
	case LatestValueExpr:
		if err := validateMany([]Expr{value.Value, value.OrderBy}); err != nil {
			return err
		}
		if value.TieBreak != nil {
			return validateExpr(value.TieBreak, aliases, rendererDialect)
		}
	case EarliestValueExpr:
		if err := validateMany([]Expr{value.Value, value.OrderBy}); err != nil {
			return err
		}
		if value.TieBreak != nil {
			return validateExpr(value.TieBreak, aliases, rendererDialect)
		}
	case WindowExpr:
		if err := validateExpr(value.Function, aliases, rendererDialect); err != nil {
			return err
		}
		if err := validateMany(value.PartitionBy); err != nil {
			return err
		}
		if err := validateMany(value.OrderBy); err != nil {
			return err
		}
		if value.Frame != WindowRowsUnboundedPrecedingToCurrent && value.Frame != WindowRowsPrecedingToCurrent {
			return fmt.Errorf("unsupported window frame %q", value.Frame)
		}
		if value.Frame == WindowRowsPrecedingToCurrent && value.PrecedingRows < 0 {
			return fmt.Errorf("preceding rows must not be negative")
		}
	case RowNumberExpr:
		if len(value.OrderBy) == 0 {
			return fmt.Errorf("row number ordering is required")
		}
		if err := validateMany(value.PartitionBy); err != nil {
			return err
		}
		for _, order := range value.OrderBy {
			if order.Direction != query.SortAsc && order.Direction != query.SortDesc {
				return fmt.Errorf("unsupported row number direction %q", order.Direction)
			}
			if err := validateExpr(order.Expr, aliases, rendererDialect); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported expression type %T", expr)
	}
	return nil
}
