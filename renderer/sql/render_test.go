package sql_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

type testBehavior struct{}

func (testBehavior) QuoteIdentifier(name string) string { return `"` + name + `"` }
func (testBehavior) QuoteSource(name string) (string, error) {
	return name, nil
}
func (testBehavior) NotEqualOperator() string { return " <> ?" }
func (testBehavior) RenderExpression(expr sqlplan.Expr) (string, error) {
	return "", &unsupportedExpression{expr: expr}
}
func (testBehavior) LowerPlan(plan *sqlplan.Plan) (*sqlplan.Plan, error) {
	return sqlplan.Clone(plan), nil
}
func (testBehavior) RenderLimit(limit int) string { return fmt.Sprintf("LIMIT %d\n", limit) }
func (testBehavior) CTEBehavior() sql.Behavior    { return testBehavior{} }
func (testBehavior) StatementSuffix() string      { return "" }

type unsupportedExpression struct{ expr sqlplan.Expr }

func (e *unsupportedExpression) Error() string { return "unsupported expression" }

func TestRenderIsDeterministicAndDoesNotMutateInput(t *testing.T) {
	plan := &sqlplan.Plan{Root: "root", Blocks: []sqlplan.QueryBlock{{
		ID:          "root",
		From:        sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "analytics.orders"}, Alias: "orders"},
		Projections: []sqlplan.Projection{{Expr: sqlplan.ColumnRef{Name: "amount"}, Alias: "amount"}},
		Limit:       intPointer(5),
	}}}
	before, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	first, firstParams, err := sql.Render(plan, testBehavior{})
	if err != nil {
		t.Fatal(err)
	}
	second, secondParams, err := sql.Render(plan, testBehavior{})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !reflect.DeepEqual(firstParams, secondParams) {
		t.Fatalf("Render is not deterministic: first=(%q, %#v), second=(%q, %#v)", first, firstParams, second, secondParams)
	}
	after, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("Render mutated caller-owned SQLPlan: before=%s after=%s", before, after)
	}
	if first != "SELECT\n    \"amount\" AS \"amount\"\nFROM analytics.orders AS \"orders\"\nLIMIT 5" {
		t.Fatalf("SQL = %q", first)
	}
}

func TestRenderExpressionOwnsTargetNeutralConditionalAndWindowSyntax(t *testing.T) {
	present := sqlplan.ColumnRef{Table: "events", Name: "present"}
	value := sqlplan.ColumnRef{Table: "events", Name: "value"}
	tests := []struct {
		name string
		expr sqlplan.Expr
		want string
	}{
		{
			name: "conditional",
			expr: sqlplan.CaseExpr{
				Branches: []sqlplan.CaseWhen{{
					When: sqlplan.NullTestExpr{Expr: present, Negated: true},
					Then: sqlplan.ParenthesizedExpr{Expr: sqlplan.BinaryExpr{Left: value, Operator: "+", Right: sqlplan.OpaqueExpr{SQL: "1"}}},
				}},
				Else: sqlplan.OpaqueExpr{SQL: "NULL"},
			},
			want: `CASE WHEN "events"."present" IS NOT NULL THEN ("events"."value" + 1) ELSE NULL END`,
		},
		{
			name: "intermediate decimal precision",
			expr: sqlplan.CastExpr{Expr: value, Type: sqlplan.CastDecimal20Scale12},
			want: `CAST("events"."value" AS DECIMAL(20,12))`,
		},
		{
			name: "null on zero division",
			expr: sqlplan.NullOnZeroDivideExpr{Numerator: value, Denominator: sqlplan.ColumnRef{Table: "events", Name: "count"}},
			want: `"events"."value" / NULLIF("events"."count", 0)`,
		},
		{
			name: "bounded window",
			expr: sqlplan.WindowExpr{
				Function:      sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{value}},
				OrderBy:       []sqlplan.Expr{sqlplan.ColumnRef{Name: "month"}},
				Frame:         sqlplan.WindowRowsPrecedingToCurrent,
				PrecedingRows: 2,
			},
			want: `SUM("events"."value") OVER (ORDER BY "month" ROWS BETWEEN 2 PRECEDING AND CURRENT ROW)`,
		},
		{
			name: "row number",
			expr: sqlplan.RowNumberExpr{
				PartitionBy: []sqlplan.Expr{sqlplan.ColumnRef{Name: "conversion_id"}},
				OrderBy: []sqlplan.Order{
					{Expr: sqlplan.ColumnRef{Name: "base_time"}, Direction: query.SortDesc},
					{Expr: sqlplan.ColumnRef{Name: "base_id"}, Direction: query.SortDesc},
				},
			},
			want: `ROW_NUMBER() OVER (PARTITION BY "conversion_id" ORDER BY "base_time" DESC, "base_id" DESC)`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sql.RenderExpression(testBehavior{}, tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("expression = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderExpressionRejectsWindowFunctionsWithoutOrder(t *testing.T) {
	for _, expr := range []sqlplan.Expr{
		sqlplan.WindowExpr{Function: sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{sqlplan.ColumnRef{Name: "value"}}}, Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent},
		sqlplan.RowNumberExpr{PartitionBy: []sqlplan.Expr{sqlplan.ColumnRef{Name: "conversion_id"}}},
	} {
		if _, err := sql.RenderExpression(testBehavior{}, expr); err == nil {
			t.Fatalf("RenderExpression(%T) accepted missing order", expr)
		}
	}
}

func intPointer(value int) *int { return &value }
