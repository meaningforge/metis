package manifest_test

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

func TestBuildSemanticGraphDeterministicallyRebuildsMetricTopology(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(derivedMetricModel))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}

	first, err := manifest.BuildSemanticGraph(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manifest.BuildSemanticGraph(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	if first.ManifestDigest() != semanticManifest.Digest || second.ManifestDigest() != semanticManifest.Digest {
		t.Fatalf("graph digest = %q / %q, want %q", first.ManifestDigest(), second.ManifestDigest(), semanticManifest.Digest)
	}

	firstProject, err := first.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	secondProject, err := second.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	firstModel, err := firstProject.Model("sales")
	if err != nil {
		t.Fatal(err)
	}
	secondModel, err := secondProject.Model("sales")
	if err != nil {
		t.Fatal(err)
	}

	firstDependencies, ok := firstModel.DirectMetricDependencies("margin_per_order")
	if !ok {
		t.Fatal("margin_per_order dependencies are missing")
	}
	secondDependencies, ok := secondModel.DirectMetricDependencies("margin_per_order")
	if !ok {
		t.Fatal("rebuild lost margin_per_order dependencies")
	}
	wantDependencies := []string{"gross_margin", "orders_count"}
	if !reflect.DeepEqual(firstDependencies, wantDependencies) || !reflect.DeepEqual(secondDependencies, wantDependencies) {
		t.Fatalf("metric dependencies = %v / %v, want %v", firstDependencies, secondDependencies, wantDependencies)
	}

	order, err := firstModel.MetricEvaluationOrder("margin_per_order")
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"cost", "revenue", "gross_margin", "orders_count", "margin_per_order"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("metric evaluation order = %v, want %v", order, wantOrder)
	}

	firstDependencies[0] = "mutated"
	got, _ := firstModel.DirectMetricDependencies("margin_per_order")
	if !reflect.DeepEqual(got, wantDependencies) {
		t.Fatalf("graph exposed mutable metric dependencies: %v", got)
	}
}

func TestSemanticGraphPreservesManifestRelationshipTopology(t *testing.T) {
	semanticManifest := &manifest.SemanticManifest{
		Digest: "sha256:semantic-graph",
		Projects: map[string]*manifest.ProjectIndex{
			"test": {
				Name: "test",
				Models: map[string]*manifest.ModelIndex{
					"sales": {
						Model: &ossie.SemanticModel{Name: "sales"},
						Relationships: map[string]*ossie.Relationship{
							"orders_to_customer": relationship("orders_to_customer", "orders", "customer"),
							"customer_to_region": relationship("customer_to_region", "customer", "region"),
						},
						MetricDependencies: map[string]manifest.MetricDependency{},
					},
				},
			},
		},
	}

	graph, err := manifest.BuildSemanticGraph(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	project, err := graph.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := project.Model("sales")
	if err != nil {
		t.Fatal(err)
	}
	if !model.CanReachDirected("orders", "region") || model.CanReachDirected("region", "orders") {
		t.Fatal("graph changed manifest directed reachability")
	}

	path, err := model.ShortestRelationshipPath("orders", "region")
	if err != nil {
		t.Fatal(err)
	}
	if got := relationshipEdgeNames(path); !reflect.DeepEqual(got, []string{"orders_to_customer", "customer_to_region"}) {
		t.Fatalf("relationship path = %v", got)
	}

	edges := model.RelationshipEdges()
	edges[0].From = "mutated"
	edges[0].FromColumns[0] = "mutated"
	stable := model.RelationshipEdges()
	if stable[0].From == "mutated" || stable[0].FromColumns[0] == "mutated" {
		t.Fatalf("graph exposed mutable relationship topology: %#v", stable[0])
	}
}

func relationshipEdgeNames(edges []manifest.RelationshipEdge) []string {
	names := make([]string, len(edges))
	for index, edge := range edges {
		names[index] = edge.Name
	}
	return names
}
