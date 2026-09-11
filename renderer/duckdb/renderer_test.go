package duckdb

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

func TestQuoteIdentifierAndQualifiedSource(t *testing.T) {
	if got := (Renderer{}).QuoteIdentifier(`order"item`); got != `"order""item"` {
		t.Fatalf("quoted identifier = %q", got)
	}
	got, err := (Renderer{}).QuoteSource("sales.public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if got != `"sales"."public"."orders"` {
		t.Fatalf("quoted source = %q", got)
	}
}

func TestRenderExpressionPreservesDateAndTimestampTypes(t *testing.T) {
	renderer := Renderer{}
	column := sqlplan.ColumnRef{Table: "events", Name: "occurred_at"}
	cases := []struct {
		name string
		expr sqlplan.Expr
		want string
	}{
		{name: "month truncation", expr: sqlplan.TimeGrainExpr{Grain: query.TimeGrainMonth, Expr: column}, want: `CAST(DATE_TRUNC('MONTH', "events"."occurred_at") AS DATE)`},
		{name: "hour truncation", expr: sqlplan.TimeGrainExpr{Grain: query.TimeGrainHour, Expr: column}, want: `DATE_TRUNC('HOUR', "events"."occurred_at")`},
		{name: "day shift", expr: sqlplan.CalendarShiftExpr{Expr: column, Count: 1, Unit: query.TimeGrainDay}, want: `CAST("events"."occurred_at" + INTERVAL '1' DAY AS DATE)`},
		{name: "hour shift", expr: sqlplan.CalendarShiftExpr{Expr: column, Count: 1, Unit: query.TimeGrainHour}, want: `"events"."occurred_at" + INTERVAL '1' HOUR`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderer.RenderExpression(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("expression = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderStructurallyLowersEarliestValue(t *testing.T) {
	plan := &sqlplan.Plan{
		Root: "root",
		Blocks: []sqlplan.QueryBlock{{
			ID:   "root",
			From: sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "analytics.inventory"}, Alias: "inventory"},
			Projections: []sqlplan.Projection{
				{Expr: sqlplan.ColumnRef{Table: "inventory", Name: "warehouse"}, Alias: "warehouse"},
				{Expr: sqlplan.EarliestValueExpr{Value: sqlplan.ColumnRef{Table: "inventory", Name: "quantity"}, OrderBy: sqlplan.ColumnRef{Table: "inventory", Name: "snapshot_date"}}, Alias: "balance"},
			},
			GroupBy: []sqlplan.Expr{sqlplan.ColumnRef{Table: "inventory", Name: "warehouse"}},
		}},
	}
	query, err := (Renderer{}).Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`MIN("inventory"."snapshot_date")`, `"__metis_ordered_key"`, `"inventory"."quantity" AS "balance"`} {
		if !strings.Contains(query.SQL, fragment) {
			t.Fatalf("ordered-value SQL missing %q:\n%s", fragment, query.SQL)
		}
	}
}
