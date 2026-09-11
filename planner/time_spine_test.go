package planner_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestPlannerCarriesResolvedTimeSpineAsDenseCalendar(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	doc.SemanticModel[0].CustomExtensions = append(doc.SemanticModel[0].CustomExtensions, ossie.CustomExtension{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"time_spine","dataset":"calendar","time_dimension":"day","grains":["day","week","month","quarter","year"]}`})
	snapshot, err := manifest.BuildProjectManifest("metricflow-reference", doc)
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New(manifest.NewStore(snapshot))
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: "cumulative_revenue"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
	resolved, err := r.ResolveForRenderer(context.Background(), q, mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.DenseCalendar == nil {
		t.Fatal("expected dense calendar plan")
	}
	if plan.DenseCalendar.Dataset.Name != "calendar" || plan.DenseCalendar.Dataset.Source != "analytics.calendar" {
		t.Fatalf("dense calendar dataset = %#v", plan.DenseCalendar.Dataset)
	}
	if plan.DenseCalendar.TimeField == nil || plan.DenseCalendar.TimeField.Name != "day" {
		t.Fatalf("dense calendar time field = %#v", plan.DenseCalendar.TimeField)
	}
	if plan.DenseCalendar.Grain != query.TimeGrainMonth {
		t.Fatalf("dense calendar grain = %q", plan.DenseCalendar.Grain)
	}
	if plan.DenseCalendar.TimeExpression == "" {
		t.Fatal("dense calendar expression is empty")
	}
}
