package sqlkit

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

// Behavior supplies every physical choice for which the shared traversal has
// no target-neutral answer. Implementations must treat LowerPlan's input as
// immutable and return owned state.
type Behavior interface {
	QuoteIdentifier(string) string
	QuoteSource(string) (string, error)
	NotEqualOperator() string
	RenderExpression(sqlplan.Expr) (string, error)
	LowerPlan(*sqlplan.Plan) (*sqlplan.Plan, error)
	RenderLimit(int) string
	CTEBehavior() Behavior
	StatementSuffix() string
}

// CastBehavior is an optional physical-cast specialization. A Renderer uses
// it only when standard CAST cannot preserve the closed SQLPlan cast contract,
// for example ClickHouse nullable/non-finite Decimal conversion. New engines
// inherit standard CAST without implementing this interface.
type CastBehavior interface {
	RenderCast(string, sqlplan.CastType) (string, error)
}

// Validate verifies that plan contains only expressions compatible with the
// concrete Renderer dialect before shared traversal begins.
func Validate(plan *sqlplan.Plan, dialect string) error {
	if err := sqlplan.ValidateForRenderer(plan, dialect); err != nil {
		return fmt.Errorf("invalid SQLPlan: %w", err)
	}
	return nil
}

// Render deterministically renders a validated SQLPlan with behavior. It does
// not mutate plan; target-specific lowering must return an owned clone.
func Render(plan *sqlplan.Plan, behavior Behavior) (string, []sql.QueryParameter, error) {
	if behavior == nil {
		return "", nil, fmt.Errorf("SQL rendering behavior is required")
	}
	if err := sqlplan.Validate(plan); err != nil {
		return "", nil, fmt.Errorf("invalid SQLPlan: %w", err)
	}
	lowered, err := behavior.LowerPlan(plan)
	if err != nil {
		return "", nil, err
	}
	if err := sqlplan.Validate(lowered); err != nil {
		return "", nil, fmt.Errorf("renderer lowering produced invalid SQLPlan: %w", err)
	}
	state := renderState{blocks: make(map[sqlplan.QueryBlockID]sqlplan.QueryBlock, len(lowered.Blocks))}
	for _, block := range lowered.Blocks {
		state.blocks[block.ID] = block
	}
	return state.renderBlock(behavior, state.blocks[lowered.Root], nil)
}

type renderState struct {
	blocks map[sqlplan.QueryBlockID]sqlplan.QueryBlock
}

func (state renderState) renderBlock(behavior Behavior, block sqlplan.QueryBlock, outer map[sqlplan.QueryBlockID]string) (string, []sql.QueryParameter, error) {
	inputs := make(map[string]sqlplan.QueryInput, len(block.Inputs))
	visible := make(map[sqlplan.QueryBlockID]string, len(outer)+len(block.Inputs))
	for id, alias := range outer {
		visible[id] = alias
	}
	for _, input := range block.Inputs {
		inputs[input.Alias] = input
		if input.Mode == sqlplan.QueryInputCTE {
			if _, exists := visible[input.Block]; !exists {
				visible[input.Block] = input.Alias
			}
		}
	}

	params := make([]sql.QueryParameter, 0)
	ctes := make([]sqlplan.QueryInput, 0, len(block.Inputs))
	for _, input := range block.Inputs {
		if input.Mode != sqlplan.QueryInputCTE {
			continue
		}
		if _, alreadyVisible := outer[input.Block]; alreadyVisible {
			continue
		}
		ctes = append(ctes, input)
	}
	var b strings.Builder
	if len(ctes) > 0 {
		b.WriteString("WITH\n")
		for i, input := range ctes {
			dependency, ok := state.blocks[input.Block]
			if !ok {
				return "", nil, fmt.Errorf("query input %q references missing block %q", input.Alias, input.Block)
			}
			rendered, values, err := state.renderBlock(behavior.CTEBehavior(), dependency, visible)
			if err != nil {
				return "", nil, err
			}
			params = append(params, values...)
			b.WriteString("    " + behavior.QuoteIdentifier(input.Alias) + " AS (\n" + indentSQL(rendered, "        ") + "\n    )")
			if i < len(ctes)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
	}
	body, values, err := state.renderBlockBody(behavior, block, inputs, visible)
	if err != nil {
		return "", nil, err
	}
	params = append(params, values...)
	b.WriteString(body)
	return strings.TrimSpace(b.String()), params, nil
}

func (state renderState) renderBlockBody(behavior Behavior, block sqlplan.QueryBlock, inputs map[string]sqlplan.QueryInput, visible map[sqlplan.QueryBlockID]string) (string, []sql.QueryParameter, error) {
	params := make([]sql.QueryParameter, 0)
	var b strings.Builder
	b.WriteString("SELECT\n")
	for i, item := range block.Projections {
		rendered, err := RenderExpression(behavior, item.Expr)
		if err != nil {
			return "", nil, err
		}
		b.WriteString("    " + rendered)
		if item.Alias != "" {
			b.WriteString(" AS " + behavior.QuoteIdentifier(item.Alias))
		}
		if i < len(block.Projections)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}

	from, values, err := state.renderRelation(behavior, block.From, inputs, visible)
	if err != nil {
		return "", nil, err
	}
	params = append(params, values...)
	b.WriteString("FROM " + from)
	if block.From.Alias != "" {
		b.WriteString(" AS " + behavior.QuoteIdentifier(block.From.Alias))
	}
	b.WriteString("\n")

	for _, join := range block.Joins {
		joined, values, err := state.renderRelation(behavior, join.Relation, inputs, visible)
		if err != nil {
			return "", nil, err
		}
		params = append(params, values...)
		b.WriteString(renderJoinKeyword(join.Kind) + " " + joined)
		if join.Relation.Alias != "" {
			b.WriteString(" AS " + behavior.QuoteIdentifier(join.Relation.Alias))
		}
		if join.Kind != sqlplan.JoinCross {
			on, err := RenderExpression(behavior, join.On)
			if err != nil {
				return "", nil, err
			}
			b.WriteString(" ON " + on)
		}
		b.WriteString("\n")
	}

	if len(block.Predicates) > 0 {
		b.WriteString("WHERE ")
		for i, predicate := range block.Predicates {
			if i > 0 {
				b.WriteString(" AND ")
			}
			left, err := RenderExpression(behavior, predicate.Left)
			if err != nil {
				return "", nil, err
			}
			fragment, values, err := renderPredicate(behavior, predicate.Operator, predicate.Values)
			if err != nil {
				return "", nil, err
			}
			b.WriteString(left + fragment)
			for _, value := range values {
				params = append(params, sql.QueryParameter{Value: value})
			}
		}
		b.WriteString("\n")
	}

	if len(block.GroupBy) > 0 {
		b.WriteString("GROUP BY ")
		for i, expr := range block.GroupBy {
			if i > 0 {
				b.WriteString(", ")
			}
			rendered, err := RenderExpression(behavior, expr)
			if err != nil {
				return "", nil, err
			}
			b.WriteString(rendered)
		}
		b.WriteString("\n")
	}

	if len(block.OrderBy) > 0 {
		b.WriteString("ORDER BY ")
		for i, order := range block.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			rendered, err := RenderExpression(behavior, order.Expr)
			if err != nil {
				return "", nil, err
			}
			b.WriteString(rendered + " " + strings.ToUpper(string(order.Direction)))
		}
		b.WriteString("\n")
	}

	if block.Limit != nil {
		b.WriteString(behavior.RenderLimit(*block.Limit))
	}
	b.WriteString(behavior.StatementSuffix())
	return strings.TrimSpace(b.String()), params, nil
}

func (state renderState) renderRelation(behavior Behavior, relation sqlplan.RelationRef, inputs map[string]sqlplan.QueryInput, visible map[sqlplan.QueryBlockID]string) (string, []sql.QueryParameter, error) {
	if source := relation.FilteredSource; source != nil {
		name, err := behavior.QuoteSource(source.Name)
		if err != nil {
			return "", nil, err
		}
		parts := make([]string, 0, len(source.Predicates))
		var params []sql.QueryParameter
		for _, predicate := range source.Predicates {
			column, err := RenderExpression(behavior, predicate.Left)
			if err != nil {
				return "", nil, err
			}
			fragment, values, err := renderPredicate(behavior, predicate.Operator, predicate.Values)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, "("+column+fragment+")")
			for _, value := range values {
				params = append(params, sql.QueryParameter{Value: value})
			}
		}
		return "(SELECT * FROM " + name + " WHERE " + strings.Join(parts, " AND ") + ")", params, nil
	}
	if relation.Source != nil {
		rendered, err := behavior.QuoteSource(relation.Source.Name)
		return rendered, nil, err
	}
	if relation.Input == nil {
		return "", nil, fmt.Errorf("relation has no source or query input")
	}
	input, ok := inputs[relation.Input.Alias]
	if !ok {
		return "", nil, fmt.Errorf("undeclared query input %q", relation.Input.Alias)
	}
	if input.Mode == sqlplan.QueryInputCTE {
		return behavior.QuoteIdentifier(input.Alias), nil, nil
	}
	dependency, ok := state.blocks[input.Block]
	if !ok {
		return "", nil, fmt.Errorf("query input %q references missing block %q", input.Alias, input.Block)
	}
	rendered, params, err := state.renderBlock(behavior.CTEBehavior(), dependency, visible)
	if err != nil {
		return "", nil, err
	}
	return "(\n" + indentSQL(rendered, "    ") + "\n)", params, nil
}

// RenderExpression renders shared typed SQLPlan expressions. It delegates
// unknown physical forms to Behavior.RenderExpression, which must recurse via
// this function whenever it needs shared child rendering.
func RenderExpression(behavior Behavior, expr sqlplan.Expr) (string, error) {
	if err := rejectDegenerateExpr(expr); err != nil {
		return "", err
	}
	switch e := expr.(type) {
	case sqlplan.OpaqueExpr:
		return e.SQL, nil
	case sqlplan.ColumnRef:
		if e.Table == "" {
			return behavior.QuoteIdentifier(e.Name), nil
		}
		return behavior.QuoteIdentifier(e.Table) + "." + behavior.QuoteIdentifier(e.Name), nil
	case sqlplan.BinaryExpr:
		left, err := RenderExpression(behavior, e.Left)
		if err != nil {
			return "", err
		}
		right, err := RenderExpression(behavior, e.Right)
		if err != nil {
			return "", err
		}
		return left + " " + e.Operator + " " + right, nil
	case sqlplan.NullOnZeroDivideExpr:
		numerator, err := RenderExpression(behavior, e.Numerator)
		if err != nil {
			return "", err
		}
		denominator, err := RenderExpression(behavior, e.Denominator)
		if err != nil {
			return "", err
		}
		return numerator + " / NULLIF(" + denominator + ", 0)", nil
	case sqlplan.LogicalExpr:
		parts := make([]string, 0, len(e.Terms))
		for _, term := range e.Terms {
			rendered, err := RenderExpression(behavior, term)
			if err != nil {
				return "", err
			}
			parts = append(parts, rendered)
		}
		return strings.Join(parts, " "+strings.ToUpper(e.Operator)+" "), nil
	case sqlplan.FunctionCallExpr:
		args := make([]string, 0, len(e.Args))
		for _, arg := range e.Args {
			rendered, err := RenderExpression(behavior, arg)
			if err != nil {
				return "", err
			}
			args = append(args, rendered)
		}
		return strings.ToUpper(e.Name) + "(" + strings.Join(args, ", ") + ")", nil
	case sqlplan.NullTestExpr:
		nested, err := RenderExpression(behavior, e.Expr)
		if err != nil {
			return "", err
		}
		if e.Negated {
			return nested + " IS NOT NULL", nil
		}
		return nested + " IS NULL", nil
	case sqlplan.CaseExpr:
		var b strings.Builder
		b.WriteString("CASE")
		for _, branch := range e.Branches {
			when, err := RenderExpression(behavior, branch.When)
			if err != nil {
				return "", err
			}
			then, err := RenderExpression(behavior, branch.Then)
			if err != nil {
				return "", err
			}
			b.WriteString(" WHEN ")
			b.WriteString(when)
			b.WriteString(" THEN ")
			b.WriteString(then)
		}
		otherwise, err := RenderExpression(behavior, e.Else)
		if err != nil {
			return "", err
		}
		b.WriteString(" ELSE ")
		b.WriteString(otherwise)
		b.WriteString(" END")
		return b.String(), nil
	case sqlplan.ParenthesizedExpr:
		nested, err := RenderExpression(behavior, e.Expr)
		if err != nil {
			return "", err
		}
		return "(" + nested + ")", nil
	case sqlplan.CastExpr:
		nested, err := RenderExpression(behavior, e.Expr)
		if err != nil {
			return "", err
		}
		if e.Type != sqlplan.CastDecimal20Scale12 && e.Type != sqlplan.CastDecimal38Scale18 {
			return "", fmt.Errorf("unsupported cast type %q", e.Type)
		}
		if renderer, ok := behavior.(CastBehavior); ok {
			return renderer.RenderCast(nested, e.Type)
		}
		return "CAST(" + nested + " AS " + string(e.Type) + ")", nil
	case sqlplan.WindowExpr:
		return renderWindowExpression(behavior, e)
	case sqlplan.RowNumberExpr:
		return renderRowNumberExpression(behavior, e)
	default:
		return behavior.RenderExpression(expr)
	}
}

func renderPredicate(behavior Behavior, op query.FilterOperator, values []any) (string, []any, error) {
	switch op {
	case query.FilterEQ:
		return " = ?", values, nil
	case query.FilterNEQ:
		return behavior.NotEqualOperator(), values, nil
	case query.FilterGT:
		return " > ?", values, nil
	case query.FilterGTE:
		return " >= ?", values, nil
	case query.FilterLT:
		return " < ?", values, nil
	case query.FilterLTE:
		return " <= ?", values, nil
	case query.FilterIN, query.FilterNotIn:
		if len(values) == 0 {
			return "", nil, fmt.Errorf("%s requires at least one value", op)
		}
		keyword := " IN ("
		if op == query.FilterNotIn {
			keyword = " NOT IN ("
		}
		return keyword + strings.TrimSuffix(strings.Repeat("?, ", len(values)), ", ") + ")", values, nil
	case query.FilterBetween:
		if len(values) != 2 {
			return "", nil, fmt.Errorf("between requires exactly two values")
		}
		return " BETWEEN ? AND ?", values, nil
	case query.FilterIsNull:
		return " IS NULL", nil, nil
	case query.FilterIsNotNull:
		return " IS NOT NULL", nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported filter operator %q", op)
	}
}

func rejectDegenerateExpr(expr sqlplan.Expr) error {
	switch e := expr.(type) {
	case sqlplan.LogicalExpr:
		if len(e.Terms) == 0 {
			return fmt.Errorf("logical expression requires terms")
		}
	case sqlplan.FunctionCallExpr:
		if strings.TrimSpace(e.Name) == "" {
			return fmt.Errorf("function name is required")
		}
	}
	return nil
}

func renderWindowExpression(behavior Behavior, e sqlplan.WindowExpr) (string, error) {
	fn, err := RenderExpression(behavior, e.Function)
	if err != nil {
		return "", err
	}
	parts := []string{}
	if len(e.PartitionBy) > 0 {
		xs := []string{}
		for _, x := range e.PartitionBy {
			rendered, err := RenderExpression(behavior, x)
			if err != nil {
				return "", err
			}
			xs = append(xs, rendered)
		}
		parts = append(parts, "PARTITION BY "+strings.Join(xs, ", "))
	}
	if len(e.OrderBy) == 0 {
		return "", fmt.Errorf("window expression requires order by")
	}
	xs := []string{}
	for _, x := range e.OrderBy {
		rendered, err := RenderExpression(behavior, x)
		if err != nil {
			return "", err
		}
		xs = append(xs, rendered)
	}
	parts = append(parts, "ORDER BY "+strings.Join(xs, ", "))
	switch e.Frame {
	case sqlplan.WindowRowsUnboundedPrecedingToCurrent:
		parts = append(parts, "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW")
	case sqlplan.WindowRowsPrecedingToCurrent:
		if e.PrecedingRows < 0 {
			return "", fmt.Errorf("bounded window preceding rows must be non-negative")
		}
		parts = append(parts, fmt.Sprintf("ROWS BETWEEN %d PRECEDING AND CURRENT ROW", e.PrecedingRows))
	default:
		return "", fmt.Errorf("unsupported window frame %q", e.Frame)
	}
	return fn + " OVER (" + strings.Join(parts, " ") + ")", nil
}

func renderRowNumberExpression(behavior Behavior, e sqlplan.RowNumberExpr) (string, error) {
	parts := []string{}
	if len(e.PartitionBy) > 0 {
		xs := make([]string, 0, len(e.PartitionBy))
		for _, x := range e.PartitionBy {
			rendered, err := RenderExpression(behavior, x)
			if err != nil {
				return "", err
			}
			xs = append(xs, rendered)
		}
		parts = append(parts, "PARTITION BY "+strings.Join(xs, ", "))
	}
	if len(e.OrderBy) == 0 {
		return "", fmt.Errorf("row-number expression requires order by")
	}
	xs := make([]string, 0, len(e.OrderBy))
	for _, order := range e.OrderBy {
		rendered, err := RenderExpression(behavior, order.Expr)
		if err != nil {
			return "", err
		}
		xs = append(xs, rendered+" "+strings.ToUpper(string(order.Direction)))
	}
	parts = append(parts, "ORDER BY "+strings.Join(xs, ", "))
	return "ROW_NUMBER() OVER (" + strings.Join(parts, " ") + ")", nil
}

func renderJoinKeyword(kind sqlplan.JoinKind) string {
	switch kind {
	case sqlplan.JoinFullOuter:
		return "FULL OUTER JOIN"
	case sqlplan.JoinCross:
		return "CROSS JOIN"
	default:
		return "JOIN"
	}
}

func indentSQL(value, indent string) string {
	return indent + strings.ReplaceAll(value, "\n", "\n"+indent)
}
