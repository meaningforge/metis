// Package duckdb implements physical SQL rendering for DuckDB.
package duckdb

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

const Dialect sql.SQLDialect = "DUCKDB"

type Renderer struct{}

var (
	_ renderer.Renderer = (*Renderer)(nil)
	_ sql.Behavior      = Renderer{}
)

func New() *Renderer { return &Renderer{} }

func (Renderer) SQLDialect() sql.SQLDialect { return Dialect }
func (Renderer) ExpressionDialect() string  { return string(Dialect) }
func (Renderer) Capabilities() renderer.Capabilities {
	return renderer.Capabilities{}
}
func (Renderer) Render(plan *sqlplan.Plan) (sql.SqlStatement, error) {
	if err := sql.Validate(plan, string(Dialect)); err != nil {
		return sql.SqlStatement{}, err
	}
	text, parameters, err := sql.Render(plan, Renderer{})
	return sql.SqlStatement{Dialect: Dialect, SQL: text, Parameters: parameters}, err
}

// Renderer answers the sql.Behavior questions the shared traversal
// refuses to answer for it.
func (d Renderer) LowerPlan(plan *sqlplan.Plan) (*sqlplan.Plan, error) {
	lowered, err := sql.LowerDeterministicOrderedValuePlan(plan)
	if err != nil {
		return nil, err
	}
	return lowerOrderedValuePlan(lowered)
}

func (d Renderer) RenderLimit(limit int) string {
	return fmt.Sprintf("FETCH FIRST %d ROWS ONLY\n", limit)
}

// This dialect carries no statement-scoped state, so a CTE body renders with
// the same behavior as its enclosing statement, and no statement setting
// follows the row limit.
func (d Renderer) CTEBehavior() sql.Behavior { return d }

func (d Renderer) StatementSuffix() string { return "" }

// RenderExpression owns the expression forms the shared SQLPlan traversal refuses to
// answer for any dialect: how a timestamp is truncated, how a calendar shift is
// spelled and typed, and whether an ordered-value aggregate exists at all.
func (d Renderer) RenderExpression(expr sqlplan.Expr) (string, error) {
	switch e := expr.(type) {
	case sqlplan.TimeGrainExpr:
		inner, err := sql.RenderExpression(d, e.Expr)
		if err != nil {
			return "", err
		}
		truncated := fmt.Sprintf("DATE_TRUNC('%s', %s)", strings.ToUpper(string(e.Grain)), inner)
		// DATE_TRUNC returns a timestamp under PostgreSQL semantics, which
		// DuckDB and several other portable engines follow. Metis's compiled
		// output schema declares a date-grain dimension as a date, so emitting
		// the bare call makes the DuckDB profile contradict its own contract on a
		// standard-conforming engine. The cast is only correct for grains no
		// finer than a day; an hour grain must keep its time component.
		if isDateGrain(e.Grain) {
			return fmt.Sprintf("CAST(%s AS DATE)", truncated), nil
		}
		return truncated, nil
	case sqlplan.CalendarShiftExpr:
		inner, err := sql.RenderExpression(d, e.Expr)
		if err != nil {
			return "", err
		}
		if e.Count == 0 {
			return inner, nil
		}
		shifted := fmt.Sprintf("%s + INTERVAL '%d' %s", inner, e.Count, strings.ToUpper(string(e.Unit)))
		// Adding an interval to a date widens it to a timestamp under standard
		// semantics, the same way DATE_TRUNC does. A date-grained shift of a
		// date is still a date, and the compiled output schema says so.
		if isDateGrain(e.Unit) {
			return fmt.Sprintf("CAST(%s AS DATE)", shifted), nil
		}
		return shifted, nil
	case sqlplan.LatestValueExpr, sqlplan.EarliestValueExpr:
		return "", fmt.Errorf("ordered-value expression must be structurally lowered before DuckDB rendering")
	default:
		return "", fmt.Errorf("unsupported DuckDB SQL expression %T", expr)
	}
}

func (Renderer) QuoteIdentifier(identifier string) string {
	return quoteDuckDBIdentifier(identifier)
}

// quoteSource never fails for this dialect: any dotted source is quoted
// part-wise. The error exists because ClickHouse constrains its sources.
func (Renderer) QuoteSource(source string) (string, error) {
	return quoteDuckDBSource(source), nil
}

func (Renderer) NotEqualOperator() string { return " <> ?" }

func quoteDuckDBIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
func quoteDuckDBSource(source string) string {
	parts := strings.Split(source, ".")
	for i, part := range parts {
		parts[i] = quoteDuckDBIdentifier(part)
	}
	return strings.Join(parts, ".")
}

// isDateGrain reports whether truncating to this grain yields a value with no
// meaningful time component, and may therefore be narrowed to a date.
func isDateGrain(grain query.TimeGrain) bool {
	switch grain {
	case query.TimeGrainYear, query.TimeGrainQuarter, query.TimeGrainMonth,
		query.TimeGrainWeek, query.TimeGrainDay:
		return true
	}
	return false
}
