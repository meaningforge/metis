package conversion

import (
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/sqlplan"
)

func TestRawSQLPlanExpressionNormalizesTargetDialectEvidence(t *testing.T) {
	got, err := rawSQLPlanExpression(
		expression.ResolvedExpression{SourceDialect: "CLICKHOUSE", Source: "sumOrNull(amount)"},
		"amount",
		"CLICKHOUSE",
	)
	if err != nil {
		t.Fatal(err)
	}
	opaque, ok := got.(sqlplan.OpaqueExpr)
	if !ok {
		t.Fatalf("expression = %T, want sqlplan.OpaqueExpr", got)
	}
	if opaque.Dialect != "CLICKHOUSE" {
		t.Fatalf("dialect evidence = %q, want normalized target dialect", opaque.Dialect)
	}
}
