package ossie

import (
	"strings"
	"testing"
)

func TestLoaderAcceptsMetisExtendedExpressionDialects(t *testing.T) {
	model := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects:
                - {dialect: ANSI_SQL, expression: amount}
          - name: status
            datatype: String
            expression:
              dialects:
                - {dialect: ANSI_SQL, expression: status}
    metrics:
      - name: paid_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"}
            - {dialect: CLICKHOUSE, expression: "sumIf(amount, status = 'paid')"}
            - {dialect: DORIS, expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"}
`
	doc, err := NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	metric := doc.SemanticModel[0].Metrics[0]
	if len(metric.Expression.Dialects) != 3 {
		t.Fatalf("dialects = %#v", metric.Expression.Dialects)
	}
	if metric.Expression.Dialects[1].Dialect != DialectClickHouse {
		t.Fatalf("clickhouse dialect = %q", metric.Expression.Dialects[1].Dialect)
	}
	if metric.Expression.Dialects[2].Dialect != DialectDoris {
		t.Fatalf("doris dialect = %q", metric.Expression.Dialects[2].Dialect)
	}
}

func TestLoaderStillRejectsUnknownExpressionDialect(t *testing.T) {
	model := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects:
                - {dialect: MYSQL, expression: amount}
`
	_, err := NewLoader().Load([]byte(model))
	if err == nil || !strings.Contains(err.Error(), "unsupported expression dialect") {
		t.Fatalf("expected unsupported expression dialect error, got %v", err)
	}
}
