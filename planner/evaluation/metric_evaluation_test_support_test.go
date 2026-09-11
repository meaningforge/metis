package evaluation_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
)

const metricEvaluationModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: finance
    datasets:
      - name: orders
        source: finance.orders
        fields:
          - name: day_id
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.day_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: discount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.discount}]
      - name: costs
        source: finance.costs
        fields:
          - name: day_id
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.day_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.amount}]
      - name: calendar
        source: finance.calendar
        primary_key: [day_id]
        fields:
          - name: day_id
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: calendar.day_id}]
            dimension: {is_time: true}
    relationships:
      - name: orders_to_calendar
        from: orders
        to: calendar
        from_columns: [day_id]
        to_columns: [day_id]
      - name: costs_to_calendar
        from: costs
        to: calendar
        from_columns: [day_id]
        to_columns: [day_id]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]
      - name: discounts
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.discount)"}]
      - name: cost
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(costs.amount)"}]
      - name: contribution_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - discounts"}]
      - name: gross_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - cost"}]
`

func resolveMetricEvaluation(t *testing.T, modelText string, semanticQuery query.SemanticQuery) *resolver.ResolvedSemanticQuery {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(modelText))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery.Project = "test"
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mustRenderer(t *testing.T, dialect string) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	return selected
}
