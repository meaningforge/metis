package agentcontext_test

import (
	"context"
	"encoding/json"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
)

func TestPhaseFDiscoveryEvidenceIsDeterministicAndBounded(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	req := service.SearchSemanticsRequest{
		Project: conformanceProject,
		Model:   "commerce",
		Query:   "revenue",
		Kinds:   []service.AssetKind{service.AssetMetric},
		Limit:   2,
	}

	first, err := discovery.SearchSemantics(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := discovery.SearchSemantics(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("discovery results changed across identical requests:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
	if len(first.Matches) == 0 || len(first.Matches) > req.Limit {
		t.Fatalf("matches = %#v, want 1..%d", first.Matches, req.Limit)
	}
	for _, match := range first.Matches {
		if len(match.MatchReasons) == 0 {
			t.Fatalf("match %q is missing deterministic match evidence", match.Qualified)
		}
	}
}

func TestPhaseFMultiMetricCompatibilityIsFailClosed(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	result, err := discovery.GetMetricsDimensions(context.Background(), service.GetMetricsDimensionsRequest{
		Project: conformanceProject,
		Model:   "commerce",
		Metrics: []string{"revenue", "inventory_balance", "revenue"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metrics) != 2 || result.Metrics[0] != "revenue" || result.Metrics[1] != "inventory_balance" {
		t.Fatalf("metrics = %#v, want ordered de-duplicated metric set", result.Metrics)
	}

	var region *service.MetricSetDimensionCompatibility
	for i := range result.Dimensions {
		if result.Dimensions[i].Name == "region" {
			region = &result.Dimensions[i]
			break
		}
	}
	if region == nil {
		t.Fatal("region compatibility evidence not found")
	}
	if len(region.PerMetric) != 2 {
		t.Fatalf("region per-metric evidence = %#v", region.PerMetric)
	}
	if region.Status == service.CompatibilityCompatible {
		t.Fatalf("region aggregate status = %q; mixed metric set must fail closed", region.Status)
	}
}
