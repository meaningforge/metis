package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

func TestSemanticManifestLookupBindsMetricDimensionAndGraphEvidence(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(derivedMetricModel))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	semanticGraph, err := manifest.BuildSemanticGraph(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := manifest.NewSemanticManifestLookup(semanticManifest, semanticGraph)
	if err != nil {
		t.Fatal(err)
	}
	if lookup.ManifestDigest() != semanticManifest.Digest {
		t.Fatalf("lookup digest = %q, want %q", lookup.ManifestDigest(), semanticManifest.Digest)
	}
	model, err := lookup.Model("test", "sales")
	if err != nil {
		t.Fatal(err)
	}

	metric, err := model.Metrics().Metric("gross_margin")
	if err != nil {
		t.Fatal(err)
	}
	if metric.Name != "gross_margin" {
		t.Fatalf("metric = %q, want gross_margin", metric.Name)
	}
	dependency, ok := model.Metrics().Dependency("gross_margin")
	if !ok || !reflect.DeepEqual(dependency.Metrics, []string{"cost", "revenue"}) {
		t.Fatalf("gross_margin dependency = %#v", dependency)
	}
	field, err := model.Dimensions().Field("orders.revenue_amount")
	if err != nil {
		t.Fatal(err)
	}
	if field.Dataset != "orders" || field.Field.Name != "revenue_amount" {
		t.Fatalf("field = %#v", field)
	}
	order, err := model.Graph().MetricEvaluationOrder("gross_margin")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"cost", "revenue", "gross_margin"}) {
		t.Fatalf("metric evaluation order = %v", order)
	}
}

func TestSemanticManifestLookupRejectsMismatchedGraphDigest(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(derivedMetricModel))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	semanticGraph, err := manifest.BuildSemanticGraph(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest.Digest = "sha256:changed"
	_, err = manifest.NewSemanticManifestLookup(semanticManifest, semanticGraph)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("lookup error = %v, want digest mismatch", err)
	}
}
