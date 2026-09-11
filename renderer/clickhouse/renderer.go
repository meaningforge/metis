// Package clickhouse implements physical SQL rendering for ClickHouse.
package clickhouse

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/renderer/sqlkit"
	"github.com/meaningforge/metis/sqlplan"
)

const Dialect sql.SQLDialect = "CLICKHOUSE"

// ClickHouseRenderer carries one piece of statement-scoped state.
//
// ClickHouse expresses outer-join null semantics as a trailing statement
// setting rather than in the join itself, and that setting belongs to the
// statement that contains the join -- not to the bodies of its common table
// expressions. The flag is unexported so the registered zero value is still the
// correct way to construct the dialect.
type Renderer struct{ preserveJoinNulls bool }

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
	text, parameters, err := sqlkit.Render(plan, Renderer{preserveJoinNulls: containsFullOuterJoin(plan)})
	return sql.SQLQuery{Dialect: Dialect, SQL: text, Parameters: parameters}, err
}

// Renderer answers the sqlkit.Behavior questions the shared traversal
// refuses to answer for it.
//
// ClickHouse renders ordered-value aggregates natively with argMax and argMin,
// so unlike the other renderers it lowers nothing before rendering.
func (d Renderer) LowerPlan(plan *sqlplan.Plan) (*sqlplan.Plan, error) {
	return sqlplan.Clone(plan), nil
}

func (d Renderer) RenderLimit(limit int) string {
	return fmt.Sprintf("LIMIT %d\n", limit)
}

// A CTE body is a separate statement for the purposes of this setting: its own
// joins are governed by whatever it contains, not by the enclosing statement.
func (d Renderer) CTEBehavior() sqlkit.Behavior {
	return Renderer{preserveJoinNulls: false}
}

func (d Renderer) StatementSuffix() string {
	if d.preserveJoinNulls {
		return "SETTINGS join_use_nulls = 1\n"
	}
	return ""
}

// RenderCast preserves NULL and turns non-finite Float intermediates into NULL
// at the exact Decimal boundary. ClickHouse's ordinary CAST drops Nullable by
// default and rejects NaN/Inf, while Metis Decimal outputs require a bounded,
// exact value or SQL NULL.
func (Renderer) RenderCast(expr string, castType sqlplan.CastType) (string, error) {
	return "accurateCastOrNull(" + expr + ", '" + string(castType) + "')", nil
}

func containsFullOuterJoin(plan *sqlplan.Plan) bool {
	if plan == nil {
		return false
	}
	for _, block := range plan.Blocks {
		for _, join := range block.Joins {
			if join.Kind == sqlplan.JoinFullOuter {
				return true
			}
		}
	}
	return false
}

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
		if e.Grain == query.TimeGrainWeek {
			return "toStartOfWeek(" + r + ", 1)", nil
		}
		fn, er := clickHouseTimeGrainFunction(e.Grain)
		if er != nil {
			return "", er
		}
		return fn + "(" + r + ")", nil
	case sqlplan.CalendarShiftExpr:
		r, er := sqlkit.RenderExpression(d, e.Expr)
		if er != nil {
			return "", er
		}
		if e.Count == 0 {
			return r, nil
		}
		fn, er := clickHouseCalendarShiftFunction(e.Unit)
		if er != nil {
			return "", er
		}
		return fmt.Sprintf("%s(%s, %d)", fn, r, e.Count), nil
	case sqlplan.LatestValueExpr:
		v, er := sqlkit.RenderExpression(d, e.Value)
		if er != nil {
			return "", er
		}
		o, er := d.renderClickHouseOrderedKey(e.OrderBy, e.TieBreak)
		if er != nil {
			return "", er
		}
		return "argMax(" + v + ", " + o + ")", nil
	case sqlplan.EarliestValueExpr:
		v, er := sqlkit.RenderExpression(d, e.Value)
		if er != nil {
			return "", er
		}
		o, er := d.renderClickHouseOrderedKey(e.OrderBy, e.TieBreak)
		if er != nil {
			return "", er
		}
		return "argMin(" + v + ", " + o + ")", nil
	default:
		return "", fmt.Errorf("unsupported ClickHouse SQL expression %T", expr)
	}
}

func (d Renderer) renderClickHouseOrderedKey(primary, tie sqlplan.Expr) (string, error) {
	p, er := sqlkit.RenderExpression(d, primary)
	if er != nil {
		return "", er
	}
	if tie == nil {
		return p, nil
	}
	t, er := sqlkit.RenderExpression(d, tie)
	if er != nil {
		return "", er
	}
	return "tuple(" + p + ", " + t + ")", nil
}
func clickHouseTimeGrainFunction(g query.TimeGrain) (string, error) {
	switch g {
	case query.TimeGrainYear:
		return "toStartOfYear", nil
	case query.TimeGrainQuarter:
		return "toStartOfQuarter", nil
	case query.TimeGrainMonth:
		return "toStartOfMonth", nil
	case query.TimeGrainWeek:
		return "toStartOfWeek", nil
	case query.TimeGrainDay:
		return "toStartOfDay", nil
	case query.TimeGrainHour:
		return "toStartOfHour", nil
	default:
		return "", fmt.Errorf("unsupported ClickHouse time grain %q", g)
	}
}
func clickHouseCalendarShiftFunction(g query.TimeGrain) (string, error) {
	switch g {
	case query.TimeGrainHour:
		return "addHours", nil
	case query.TimeGrainDay:
		return "addDays", nil
	case query.TimeGrainWeek:
		return "addWeeks", nil
	case query.TimeGrainMonth:
		return "addMonths", nil
	case query.TimeGrainQuarter:
		return "addQuarters", nil
	case query.TimeGrainYear:
		return "addYears", nil
	default:
		return "", fmt.Errorf("unsupported ClickHouse calendar shift unit %q", g)
	}
}

func (Renderer) QuoteIdentifier(identifier string) string {
	return quoteClickHouseIdentifier(identifier)
}

func (Renderer) QuoteSource(source string) (string, error) {
	return quoteClickHouseSource(source)
}

// SQL rendering normalizes inequality to the SQL-standard <> spelling.
// ClickHouse accepts both <> and !=, so this removes extraction-time fork drift
// without changing the predicate's semantics.
func (Renderer) NotEqualOperator() string { return " <> ?" }

func quoteClickHouseSource(source string) (string, error) {
	parts := strings.Split(strings.TrimSpace(source), ".")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid ClickHouse source %q: expected database.table", source)
	}
	return quoteClickHouseIdentifier(parts[0]) + "." + quoteClickHouseIdentifier(parts[1]), nil
}
func quoteClickHouseIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}
