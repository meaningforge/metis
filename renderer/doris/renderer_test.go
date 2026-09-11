package doris

import (
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

func TestQuoteIdentifierAndQualifiedSource(t *testing.T) {
	if got := (Renderer{}).QuoteIdentifier("order`item"); got != "`order``item`" {
		t.Fatalf("quoted identifier = %q", got)
	}
	got, err := (Renderer{}).QuoteSource("sales.public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if got != "`sales`.`public`.`orders`" {
		t.Fatalf("quoted source = %q", got)
	}
}

func TestRenderExpressionOwnsDorisTimeAndOrderedValueSyntax(t *testing.T) {
	renderer := Renderer{}
	column := sqlplan.ColumnRef{Table: "events", Name: "occurred_at"}
	cases := []struct {
		name string
		expr sqlplan.Expr
		want string
	}{
		{name: "month", expr: sqlplan.TimeGrainExpr{Grain: query.TimeGrainMonth, Expr: column}, want: "DATE_TRUNC(\"month\", `events`.`occurred_at`)"},
		{name: "quarter shift", expr: sqlplan.CalendarShiftExpr{Expr: column, Count: -1, Unit: query.TimeGrainQuarter}, want: "DATE_ADD(`events`.`occurred_at`, INTERVAL -3 MONTH)"},
		{name: "earliest", expr: sqlplan.EarliestValueExpr{Value: sqlplan.ColumnRef{Name: "value"}, OrderBy: column}, want: "MIN_BY(`value`, `events`.`occurred_at`)"},
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

func TestRenderExpressionRejectsUnloweredDeterministicOrderedValue(t *testing.T) {
	_, err := (Renderer{}).RenderExpression(sqlplan.EarliestValueExpr{
		Value:    sqlplan.ColumnRef{Name: "value"},
		OrderBy:  sqlplan.ColumnRef{Name: "occurred_at"},
		TieBreak: sqlplan.ColumnRef{Name: "sequence"},
	})
	if err == nil {
		t.Fatal("unlowered deterministic ordered value was accepted")
	}
}
