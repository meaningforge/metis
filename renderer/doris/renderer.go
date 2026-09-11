// Package doris implements physical SQL rendering for Apache Doris.
package doris

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/renderer/sqlkit"
	"github.com/meaningforge/metis/sqlplan"
)

const Dialect sql.SQLDialect = "DORIS"

type Renderer struct{}

var (
	_ renderer.Renderer = (*Renderer)(nil)
	_ sqlkit.Behavior   = Renderer{}
)

func New() *Renderer { return &Renderer{} }

func (Renderer) SQLDialect() sql.SQLDialect { return Dialect }
func (Renderer) ExpressionDialect() string  { return string(Dialect) }
func (Renderer) Capabilities() renderer.Capabilities {
	return renderer.Capabilities{}
}
func (Renderer) Render(plan *sqlplan.Plan) (sql.SQLQuery, error) {
	if err := sqlkit.Validate(plan, string(Dialect)); err != nil {
		return sql.SQLQuery{}, err
	}
	text, parameters, err := sqlkit.Render(plan, Renderer{})
	return sql.SQLQuery{Dialect: Dialect, SQL: text, Parameters: parameters}, err
}

// Renderer answers the sqlkit.Behavior questions the shared traversal
// refuses to answer for it.
//
// Doris renders ordered-value aggregates natively, so it lowers only the
// deterministic tie-break rewrite and not the join rewrite the DuckDB renderer
// needs.
func (d Renderer) LowerPlan(plan *sqlplan.Plan) (*sqlplan.Plan, error) {
	return sqlkit.LowerDeterministicOrderedValuePlan(plan)
}

func (d Renderer) RenderLimit(limit int) string {
	return fmt.Sprintf("LIMIT %d\n", limit)
}

// This dialect carries no statement-scoped state, so a CTE body renders with
// the same behavior as its enclosing statement, and no statement setting
// follows the row limit.
func (d Renderer) CTEBehavior() sqlkit.Behavior { return d }

func (d Renderer) StatementSuffix() string { return "" }

// RenderExpression owns the expression forms the shared SQLPlan traversal refuses to
// answer for any dialect: how a timestamp is truncated, how a calendar shift is
// spelled, and how an ordered-value aggregate is named.
func (d Renderer) RenderExpression(expr sqlplan.Expr) (string, error) {
	switch e := expr.(type) {
	case sqlplan.TimeGrainExpr:
		r, er := sqlkit.RenderExpression(d, e.Expr)
		if er != nil {
			return "", er
		}
		return fmt.Sprintf("DATE_TRUNC(%q, %s)", string(e.Grain), r), nil
	case sqlplan.CalendarShiftExpr:
		r, er := sqlkit.RenderExpression(d, e.Expr)
		if er != nil {
			return "", er
		}
		if e.Count == 0 {
			return r, nil
		}
		count := e.Count
		unit := e.Unit
		// Doris does not accept QUARTER as a DATE_ADD interval unit. Preserve
		// quarter semantics through the equivalent fixed three-month shift.
		if unit == query.TimeGrainQuarter {
			count *= 3
			unit = query.TimeGrainMonth
		}
		return fmt.Sprintf("DATE_ADD(%s, INTERVAL %d %s)", r, count, strings.ToUpper(string(unit))), nil
	case sqlplan.LatestValueExpr:
		if e.TieBreak != nil {
			return "", fmt.Errorf("deterministic latest-value expression must be structurally lowered before Doris rendering")
		}
		v, er := sqlkit.RenderExpression(d, e.Value)
		if er != nil {
			return "", er
		}
		o, er := sqlkit.RenderExpression(d, e.OrderBy)
		if er != nil {
			return "", er
		}
		return "MAX_BY(" + v + ", " + o + ")", nil
	case sqlplan.EarliestValueExpr:
		if e.TieBreak != nil {
			return "", fmt.Errorf("deterministic earliest-value expression must be structurally lowered before Doris rendering")
		}
		v, er := sqlkit.RenderExpression(d, e.Value)
		if er != nil {
			return "", er
		}
		o, er := sqlkit.RenderExpression(d, e.OrderBy)
		if er != nil {
			return "", er
		}
		return "MIN_BY(" + v + ", " + o + ")", nil
	default:
		return "", fmt.Errorf("unsupported SQL expression %T", expr)
	}
}

func (Renderer) QuoteIdentifier(identifier string) string { return quoteIdentifier(identifier) }

// quoteSource never fails for this dialect: any dotted source is quoted
// part-wise. The error exists because ClickHouse constrains its sources.
func (Renderer) QuoteSource(source string) (string, error) { return quoteSource(source), nil }

func (Renderer) NotEqualOperator() string { return " <> ?" }

func quoteSource(source string) string {
	parts := strings.Split(source, ".")
	for i, p := range parts {
		parts[i] = quoteIdentifier(p)
	}
	return strings.Join(parts, ".")
}
func quoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}
