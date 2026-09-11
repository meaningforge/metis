package compiler_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestMetricTimeBindingRemainsCanonicalAcrossExplicitTimeDimensions(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New(manifest.NewStore(snapshot))
	renderer := mustRenderer(t, "DORIS")
	for _, dimension := range []string{"order_date", "ship_date", "payment_date"} {
		dimension := dimension
		t.Run(dimension, func(t *testing.T) {
			grain := query.TimeGrainMonth
			q := query.SemanticQuery{Project: projectName, Model: modelName, Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: dimension, Grain: &grain}}}
			resolved, err := r.ResolveForRenderer(context.Background(), q, renderer)
			if err != nil {
				t.Fatal(err)
			}
			if len(resolved.Metrics) != 1 || resolved.Metrics[0].TimeBinding == nil {
				t.Fatalf("resolved metric binding = %#v", resolved.Metrics)
			}
			if got := resolved.Metrics[0].TimeBinding.TimeDimension; got != "order_date" {
				t.Fatalf("canonical time dimension = %q, want order_date", got)
			}
			if len(resolved.Dimensions) != 1 || resolved.Dimensions[0].Field.Name != dimension {
				t.Fatalf("resolved query dimension = %#v, want %s", resolved.Dimensions, dimension)
			}
		})
	}
}

func TestMetricExtensionsCanCoexistByKind(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	var cumulative *ossie.Metric
	for i := range doc.SemanticModel[0].Metrics {
		if doc.SemanticModel[0].Metrics[i].Name == "cumulative_revenue" {
			cumulative = &doc.SemanticModel[0].Metrics[i]
			break
		}
	}
	if cumulative == nil {
		t.Fatal("cumulative_revenue fixture metric not found")
	}
	cumulativeSpec, ok, err := ossie.CumulativeSpec(cumulative)
	if err != nil || !ok {
		t.Fatalf("cumulative spec: ok=%v err=%v", ok, err)
	}
	binding, ok, err := ossie.MetricTimeBinding(cumulative)
	if err != nil || !ok {
		t.Fatalf("time binding: ok=%v err=%v", ok, err)
	}
	if cumulativeSpec.TimeDimension != "order_date" || binding.TimeDimension != "order_date" {
		t.Fatalf("extensions disagree: cumulative=%q binding=%q", cumulativeSpec.TimeDimension, binding.TimeDimension)
	}
}
