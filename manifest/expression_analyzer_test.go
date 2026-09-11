package manifest_test

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

type fixedAnalyzer struct {
	calls int
	names []string
}

func (a *fixedAnalyzer) AnalyzeMetric(metric *ossie.Metric, model *manifest.ModelIndex) (manifest.MetricAnalysis, error) {
	a.calls++
	a.names = append(a.names, metric.Name)
	return manifest.MetricAnalysis{Dependency: manifest.MetricDependency{
		Datasets:   []string{"orders"},
		References: []manifest.SemanticReference{{Dataset: "orders", Field: "amount"}},
		Inference:  "test-parser-backed",
	}}, nil
}

func TestBuildProjectManifestUsesInjectedExpressionAnalyzer(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(dependencyModel))
	if err != nil {
		t.Fatal(err)
	}
	analyzer := &fixedAnalyzer{}
	snapshot, err := manifest.BuildProjectManifestWithAnalyzer("test", doc, analyzer)
	if err != nil {
		t.Fatal(err)
	}
	project, err := snapshot.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := project.Model("sales")
	if err != nil {
		t.Fatal(err)
	}
	dep, ok := model.MetricDependency("apac_revenue")
	if !ok {
		t.Fatal("apac_revenue dependency not indexed")
	}
	if dep.Inference != "test-parser-backed" {
		t.Fatalf("inference = %q", dep.Inference)
	}
	if analyzer.calls != len(model.Metrics) {
		t.Fatalf("analyzer calls = %d, want %d", analyzer.calls, len(model.Metrics))
	}
	if want := []string{"apac_revenue", "paid_revenue"}; !reflect.DeepEqual(analyzer.names, want) {
		t.Fatalf("metric analysis order = %#v, want %#v", analyzer.names, want)
	}
}
