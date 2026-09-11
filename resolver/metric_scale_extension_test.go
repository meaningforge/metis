package resolver_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

const metricScaleModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - vendor_name: metis.semantic
            data: '{"kind":"metric_scale","version":"1","factor":2}'
`

func TestMetricScaleEvidenceSurvivesResolverPlannerAndLowersValueSemantics(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(metricScaleModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("demo", doc)
	if err != nil {
		t.Fatal(err)
	}
	store := manifest.NewStore(snapshot)
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(store).ResolveForRenderer(context.Background(), query.SemanticQuery{
		Project: "demo", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}},
	}, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Metrics) != 1 {
		t.Fatalf("resolved metrics = %#v", resolved.Metrics)
	}
	resolvedEvidence := resolved.Metrics[0].Expression.ExtensionEvidenceValues()
	if len(resolvedEvidence) != 1 {
		t.Fatalf("resolved metric evidence = %#v", resolvedEvidence)
	}
	if _, ok := resolvedEvidence[0].(extension.MetricScaleEvidence); !ok {
		t.Fatalf("evidence type = %T", resolvedEvidence[0])
	}
	if got := resolved.Metrics[0].Expression.Source; got != "(SUM(orders.amount)) * 2" {
		t.Fatalf("resolved scaled source = %q", got)
	}

	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Projections) != 1 || len(plan.Projections[0].Expression.ExtensionEvidenceValues()) != 1 {
		t.Fatalf("planner lost extension evidence: %#v", plan.Projections)
	}
	stmt, err := conversion.BuildSQLPlan(plan, renderer)
	if err != nil {
		t.Fatal(err)
	}
	var projections int
	for _, block := range stmt.Blocks {
		if block.ID == stmt.Root {
			projections = len(block.Projections)
			break
		}
	}
	if projections != 1 {
		t.Fatalf("root projections = %d, SQLPlan = %#v", projections, stmt)
	}
	if got := resolved.Metrics[0].Expression.Source; !strings.Contains(got, "* 2") {
		t.Fatalf("lowered metric did not retain semantic scale: %q", got)
	}
}
