package expression

import (
	"reflect"
	"testing"
)

func TestParserReferenceIntegrationCorpus(t *testing.T) {
	tests := []struct {
		name    string
		dialect DialectProfile
		expr    string
		want    []Reference
	}{
		{name: "count wildcard", dialect: ANSI, expr: "COUNT(*)", want: []Reference{}},
		{name: "distinct reference", dialect: ANSI, expr: "COUNT(DISTINCT customer.customer_id)", want: []Reference{{Qualifier: "customer", Name: "customer_id"}}},
		{name: "complex decimal cast", dialect: ANSI, expr: "CAST(orders.amount AS DECIMAL(18,2))", want: []Reference{{Qualifier: "orders", Name: "amount"}}},
		{name: "double-colon parameterized cast", dialect: Snowflake, expr: "payload:amount::NUMBER(18,2)", want: []Reference{{Name: "payload"}}},
		{name: "interval arithmetic", dialect: ANSI, expr: "orders.created_at + INTERVAL 7 DAY", want: []Reference{{Qualifier: "orders", Name: "created_at"}}},
		{name: "parametric aggregate", dialect: ClickHouse, expr: "quantile(0.9)(orders.amount)", want: []Reference{{Qualifier: "orders", Name: "amount"}}},
		{name: "multi parameter lambda", dialect: ClickHouse, expr: "arrayMap((x, y) -> x + y + orders.tax, orders.a, orders.b)", want: []Reference{{Qualifier: "orders", Name: "tax"}, {Qualifier: "orders", Name: "a"}, {Qualifier: "orders", Name: "b"}}},
		{name: "nested lambda shadowing", dialect: ClickHouse, expr: "arrayMap(x -> arrayMap(x -> x + orders.tax, orders.inner), orders.outer)", want: []Reference{{Qualifier: "orders", Name: "tax"}, {Qualifier: "orders", Name: "inner"}, {Qualifier: "orders", Name: "outer"}}},
		{name: "case between and not in", dialect: ANSI, expr: "CASE WHEN orders.amount BETWEEN 10 AND 20 AND orders.status NOT IN ('x','y') THEN orders.amount ELSE 0 END", want: []Reference{{Qualifier: "orders", Name: "amount"}, {Qualifier: "orders", Name: "status"}}},
		{name: "quoted identifiers", dialect: ANSI, expr: "SUM(\"orders\".\"amount\")", want: []Reference{{Qualifier: "orders", Name: "amount"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := Parse(tt.expr, tt.dialect)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.expr, err)
			}
			got := CollectReferences(ast)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("refs = %#v, want %#v", got, tt.want)
			}
		})
	}
}
