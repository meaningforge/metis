package expression

import "testing"

func TestBoundExpressionFeedsSemanticAnalyzer(t *testing.T) {
	ast, err := Parse("SUM(orders.amount) + tax", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := Bind(ast, fixedReferenceResolver{
		{Qualifier: "orders", Name: "amount"}: {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
		{Name: "tax"}:                         {Kind: BoundMetric, Name: "tax", Type: TypeDecimal, Aggregation: AggregationAggregate},
	})
	if err != nil {
		t.Fatal(err)
	}
	typed, err := NewSemanticAnalyzer(bound, nil).Analyze(bound.Expr)
	if err != nil {
		t.Fatal(err)
	}
	if typed.Type != TypeDecimal || typed.Aggregation != AggregationAggregate || len(typed.Symbols) != 2 {
		t.Fatalf("typed expression = %#v", typed)
	}
}
