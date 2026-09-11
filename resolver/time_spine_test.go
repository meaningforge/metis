package resolver_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestTimeSpineActivatesForCanonicalTimeRelativeQuery(t *testing.T) {
	r := timeSpineResolver(t)
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: "cumulative_revenue"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
	resolved, err := r.ResolveForRenderer(context.Background(), q, mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TimeSpine == nil {
		t.Fatal("expected resolved time spine")
	}
	if resolved.TimeSpine.Dataset == nil || resolved.TimeSpine.Dataset.Name != "calendar" {
		t.Fatalf("time spine dataset = %#v", resolved.TimeSpine.Dataset)
	}
	if resolved.TimeSpine.TimeField == nil || resolved.TimeSpine.TimeField.Name != "day" {
		t.Fatalf("time spine field = %#v", resolved.TimeSpine.TimeField)
	}
	if resolved.TimeSpine.RequestedGrain != query.TimeGrainMonth {
		t.Fatalf("grain = %q", resolved.TimeSpine.RequestedGrain)
	}
	if !resolved.TimeSpine.Expression.IsResolved() {
		t.Fatal("target-aware time spine expression was not selected")
	}
}

func TestTimeSpineDoesNotActivateForOrdinaryMetric(t *testing.T) {
	r := timeSpineResolver(t)
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
	resolved, err := r.ResolveForRenderer(context.Background(), q, mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TimeSpine != nil {
		t.Fatalf("ordinary metric unexpectedly activated time spine: %#v", resolved.TimeSpine)
	}
}

func TestTimeSpineActivatesForOffsetToGrainQuery(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"time_spine","dataset":"calendar","time_dimension":"day","grains":["month","year"]}'
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: order_date, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: order_date}]}, dimension: {is_time: true}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}}
      - name: calendar
        source: analytics.calendar
        primary_key: [day]
        fields:
          - {name: day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: day}]}, dimension: {is_time: true}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue)"}]}
      - name: revenue_at_year_start
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"offset_to_grain","base_metric":"revenue","time_dimension":"order_date","grain":"year"}'
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Project: "test", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue_at_year_start"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), q, mustRenderer(t, "CLICKHOUSE"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TimeSpine == nil {
		t.Fatal("expected offset-to-grain query to activate time spine")
	}
	if resolved.TimeSpine.QueryTimeDimension != "order_date" || resolved.TimeSpine.RequestedGrain != query.TimeGrainMonth {
		t.Fatalf("time spine = %#v", resolved.TimeSpine)
	}
}

func timeSpineResolver(t *testing.T) *resolver.Resolver {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.SemanticModel) == 0 {
		t.Fatal("commerce fixture has no semantic model")
	}
	doc.SemanticModel[0].CustomExtensions = append(doc.SemanticModel[0].CustomExtensions, ossie.CustomExtension{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"time_spine","dataset":"calendar","time_dimension":"day","grains":["day","week","month","quarter","year"]}`})
	snapshot, err := manifest.BuildProjectManifest("metricflow-reference", doc)
	if err != nil {
		t.Fatal(err)
	}
	return resolver.New(manifest.NewStore(snapshot))
}
