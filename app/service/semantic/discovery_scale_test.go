package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

func BenchmarkLargeSemanticEstateDiscovery(b *testing.B) {
	for _, assets := range []int{10_000, 50_000, 100_000} {
		semanticManifest := syntheticSemanticEstate(assets)
		discovery := NewDiscoveryService(manifest.NewStore(semanticManifest)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
		if err := discovery.PrepareIndex(); err != nil {
			b.Fatal(err)
		}
		index, err := discovery.discoveryIndex()
		if err != nil {
			b.Fatal(err)
		}
		agent := NewAgentSemanticService(discovery)
		limit := 10
		b.Run(fmt.Sprintf("assets_%d/build_index", assets), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := buildDiscoveryIndex(semanticManifest); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(assets), "assets")
			b.ReportMetric(float64(index.bytes)/float64(assets), "index_B/asset")
		})
		b.Run(fmt.Sprintf("assets_%d/list_models", assets), func(b *testing.B) {
			request := ListModelsRequest{ProjectID: "scale", Search: []string{"needle"}, Limit: &limit}
			b.ReportMetric(float64(assets), "assets")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := agent.ListModels(context.Background(), request); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(index.projects["scale"].modelSearch.candidates([]string{"needle"}))), "candidates")
		})
		b.Run(fmt.Sprintf("assets_%d/list_metrics", assets), func(b *testing.B) {
			request := ListMetricsRequest{ProjectID: "scale", Search: []string{"needle"}, Limit: &limit}
			b.ReportMetric(float64(assets), "assets")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := agent.ListMetrics(context.Background(), request); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(index.projects["scale"].metricSearch.candidates([]string{"needle"}))), "candidates")
		})
	}
}

func TestLargeEstateListCursorsAreGenerationBound(t *testing.T) {
	discovery := NewDiscoveryService(manifest.NewStore(syntheticSemanticEstate(100))).WithProjectAuthorizer(AllAccessProjectAuthorizer{}).WithGenerationIdentity("scale", 3)
	service := NewAgentSemanticService(discovery)
	limit := 2
	first, err := service.ListModels(context.Background(), ListModelsRequest{ProjectID: "scale", Limit: &limit})
	if err != nil || len(first.Models) != limit || first.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := service.ListModels(context.Background(), ListModelsRequest{ProjectID: "scale", Limit: &limit, Cursor: first.NextCursor})
	if err != nil || len(second.Models) != limit || first.Models[1].Ref == second.Models[0].Ref {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	discovery.WithGenerationIdentity("scale", 4)
	_, err = service.ListModels(context.Background(), ListModelsRequest{ProjectID: "scale", Limit: &limit, Cursor: first.NextCursor})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrSemanticPaginationRestart {
		t.Fatalf("err=%v, want %s", err, serrors.ErrSemanticPaginationRestart)
	}
}

func TestLargeEstateIndexMeetsFrozenCorrectnessBudgets(t *testing.T) {
	const assets = 10_000
	discovery := NewDiscoveryService(manifest.NewStore(syntheticSemanticEstate(assets))).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	if err := discovery.PrepareIndex(); err != nil {
		t.Fatal(err)
	}
	index, err := discovery.discoveryIndex()
	if err != nil {
		t.Fatal(err)
	}
	if bytesPerAsset := float64(index.bytes) / assets; bytesPerAsset > 768 {
		t.Fatalf("retained index estimate %.1f B/asset exceeds 768 B/asset", bytesPerAsset)
	}
	service := NewAgentSemanticService(discovery)
	limit := 10
	allocations := testing.AllocsPerRun(20, func() {
		result, searchErr := service.ListModels(context.Background(), ListModelsRequest{ProjectID: "scale", Search: []string{"needle"}, Limit: &limit})
		if searchErr != nil || len(result.Models) != 0 {
			panic(fmt.Sprintf("result=%#v err=%v", result, searchErr))
		}
	})
	if allocations > 64 {
		t.Fatalf("searched list_models allocations %.0f exceed 64", allocations)
	}
	metricAllocations := testing.AllocsPerRun(20, func() {
		result, searchErr := service.ListMetrics(context.Background(), ListMetricsRequest{ProjectID: "scale", Search: []string{"needle"}, Limit: &limit})
		if searchErr != nil || len(result.Metrics) != 1 {
			panic(fmt.Sprintf("result=%#v err=%v", result, searchErr))
		}
		encoded, marshalErr := json.Marshal(result)
		if marshalErr != nil || len(encoded) > agentListDetailBudgetBytes {
			panic(fmt.Sprintf("encoded bytes=%d err=%v", len(encoded), marshalErr))
		}
	})
	if metricAllocations > 160 {
		t.Fatalf("searched list_metrics allocations %.0f exceed 160", metricAllocations)
	}
}

// syntheticSemanticEstate creates exactly totalAssets assets when the input is
// divisible by ten: one model, one dataset, four dimensions, and four metrics
// per model. Names, descriptions, and map insertion order are deterministic.
func syntheticSemanticEstate(totalAssets int) *manifest.SemanticManifest {
	const assetsPerModel = 10
	models := make(map[string]*manifest.ModelIndex, totalAssets/assetsPerModel)
	for modelNumber := 0; modelNumber < totalAssets/assetsPerModel; modelNumber++ {
		modelName := fmt.Sprintf("model_%05d", modelNumber)
		datasetName := fmt.Sprintf("dataset_%05d", modelNumber)
		fields := make([]ossie.Field, 4)
		fieldIndex := make(map[string]*manifest.FieldHandle, len(fields))
		dimensions := make(map[string][]*manifest.FieldHandle, len(fields)*2)
		for fieldNumber := range fields {
			fieldName := fmt.Sprintf("dimension_%02d", fieldNumber)
			fields[fieldNumber] = ossie.Field{Name: fieldName, Description: "synthetic dimension", Dimension: &ossie.Dimension{}}
			handle := &manifest.FieldHandle{Dataset: datasetName, Field: &fields[fieldNumber]}
			qualified := datasetName + "." + fieldName
			fieldIndex[qualified] = handle
			dimensions[fieldName] = append(dimensions[fieldName], handle)
			dimensions[qualified] = []*manifest.FieldHandle{handle}
		}
		dataset := ossie.Dataset{Name: datasetName, Source: "synthetic." + datasetName, Fields: fields}
		metrics := make([]ossie.Metric, 4)
		metricIndex := make(map[string]*ossie.Metric, len(metrics))
		for metricNumber := range metrics {
			metricName := fmt.Sprintf("metric_%02d", metricNumber)
			description := "synthetic metric"
			if modelNumber == totalAssets/assetsPerModel-1 && metricNumber == len(metrics)-1 {
				description = "needle synthetic metric"
			}
			metrics[metricNumber] = ossie.Metric{Name: metricName, Description: description}
			metricIndex[metricName] = &metrics[metricNumber]
		}
		model := &ossie.SemanticModel{Name: modelName, Description: "synthetic model", Datasets: []ossie.Dataset{dataset}, Metrics: metrics}
		models[modelName] = &manifest.ModelIndex{
			Model: model, Datasets: map[string]*ossie.Dataset{datasetName: &model.Datasets[0]}, Metrics: metricIndex,
			Fields: fieldIndex, Dimensions: dimensions, Relationships: map[string]*ossie.Relationship{},
			MetricAnalyses: map[string]manifest.MetricAnalysis{}, MetricDependencies: map[string]manifest.MetricDependency{},
		}
	}
	return &manifest.SemanticManifest{Version: "0.2.0.dev0", Digest: fmt.Sprintf("sha256:synthetic-%d", totalAssets), Projects: map[string]*manifest.ProjectIndex{
		"scale": {Name: "scale", DisplayName: "scale", Models: models, Ontology: map[string]*manifest.OntologyConceptIndex{}},
	}}
}
