package resolver

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/ossie"
)

func TestSelectExpressionPrefersTargetSpecificDialect(t *testing.T) {
	tests := []struct {
		name   string
		target string
		expr   ossie.Expression
		want   string
	}{
		{
			name:   "clickhouse extension",
			target: "clickhouse",
			expr: ossie.Expression{Dialects: []ossie.DialectExpression{
				{Dialect: ossie.DialectANSISQL, Expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"},
				{Dialect: ossie.DialectDoris, Expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"},
				{Dialect: ossie.DialectClickHouse, Expression: "sumIf(amount, status = 'paid')"},
			}},
			want: "sumIf(amount, status = 'paid')",
		},
		{
			name:   "doris extension",
			target: "doris",
			expr: ossie.Expression{Dialects: []ossie.DialectExpression{
				{Dialect: ossie.DialectANSISQL, Expression: "SUM(amount)"},
				{Dialect: ossie.DialectDoris, Expression: "SUM(IF(status = 'paid', amount, NULL))"},
			}},
			want: "SUM(IF(status = 'paid', amount, NULL))",
		},
		{
			name:   "snowflake core dialect",
			target: "snowflake",
			expr: ossie.Expression{Dialects: []ossie.DialectExpression{
				{Dialect: ossie.DialectANSISQL, Expression: "ansi_expr"},
				{Dialect: ossie.DialectSnowflake, Expression: "snowflake_expr"},
			}},
			want: "snowflake_expr",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectExpression(tt.expr, tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if got.Source != tt.want {
				t.Fatalf("expression = %q, want %q", got.Source, tt.want)
			}
		})
	}
}

func TestSelectExpressionFallsBackToANSI(t *testing.T) {
	expr := ossie.Expression{Dialects: []ossie.DialectExpression{
		{Dialect: ossie.DialectANSISQL, Expression: "SUM(amount)"},
	}}
	got, err := selectExpression(expr, "clickhouse")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "SUM(amount)" || got.SourceDialect != string(ossie.DialectANSISQL) {
		t.Fatalf("expression = %#v, want ANSI fallback", got)
	}
}

func TestSelectExpressionRejectsForeignOnlyDialect(t *testing.T) {
	expr := ossie.Expression{Dialects: []ossie.DialectExpression{
		{Dialect: ossie.DialectSnowflake, Expression: "SUM(amount)::NUMBER"},
	}}
	_, err := selectExpression(expr, "clickhouse")
	if err == nil || !strings.Contains(err.Error(), "no compatible expression") {
		t.Fatalf("expected explicit compatibility error, got %v", err)
	}
}

func TestSelectExpressionDoesNotDependOnDeclarationOrder(t *testing.T) {
	first := ossie.Expression{Dialects: []ossie.DialectExpression{
		{Dialect: ossie.DialectANSISQL, Expression: "ansi_expr"},
		{Dialect: ossie.DialectSnowflake, Expression: "snowflake_expr"},
		{Dialect: ossie.DialectClickHouse, Expression: "clickhouse_expr"},
	}}
	second := ossie.Expression{Dialects: []ossie.DialectExpression{
		{Dialect: ossie.DialectClickHouse, Expression: "clickhouse_expr"},
		{Dialect: ossie.DialectSnowflake, Expression: "snowflake_expr"},
		{Dialect: ossie.DialectANSISQL, Expression: "ansi_expr"},
	}}
	for _, expr := range []ossie.Expression{first, second} {
		got, err := selectExpression(expr, "clickhouse")
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != "clickhouse_expr" {
			t.Fatalf("expression = %q, want clickhouse_expr regardless of declaration order", got.Source)
		}
	}
}
