package manifest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

func TestRelationshipGraphReturnsStableBidirectionalShortestPath(t *testing.T) {
	ab := relationship("a_to_b", "a", "b")
	bc := relationship("b_to_c", "b", "c")
	graph := manifest.NewRelationshipGraph([]*ossie.Relationship{bc, nil, ab})

	forward, err := graph.ShortestPath("a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(relationshipNames(forward), []string{"a_to_b", "b_to_c"}) {
		t.Fatalf("forward path = %v", relationshipNames(forward))
	}

	reverse, err := graph.ShortestPath("c", "a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(relationshipNames(reverse), []string{"b_to_c", "a_to_b"}) {
		t.Fatalf("reverse path = %v", relationshipNames(reverse))
	}
}

func TestRelationshipGraphReportsDeterministicAmbiguousCandidates(t *testing.T) {
	relationships := []*ossie.Relationship{
		relationship("a_to_y", "a", "y"),
		relationship("y_to_b", "y", "b"),
		relationship("a_to_x", "a", "x"),
		relationship("x_to_b", "x", "b"),
	}

	assertAmbiguousPaths := func(t *testing.T, graph *manifest.RelationshipGraph) map[string]any {
		t.Helper()
		_, err := graph.ShortestPath("a", "b")
		var apiErr *serrors.Error
		if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrAmbiguousRelationshipPath {
			t.Fatalf("error = %#v, want %s", err, serrors.ErrAmbiguousRelationshipPath)
		}
		wantRelationships := [][]string{{"a_to_x", "x_to_b"}, {"a_to_y", "y_to_b"}}
		if got := apiErr.Details["relationship_paths"]; !reflect.DeepEqual(got, wantRelationships) {
			t.Fatalf("relationship candidates = %#v, want %#v", got, wantRelationships)
		}
		wantDatasets := [][]string{{"a", "x", "b"}, {"a", "y", "b"}}
		if got := apiErr.Details["dataset_paths"]; !reflect.DeepEqual(got, wantDatasets) {
			t.Fatalf("dataset candidates = %#v, want %#v", got, wantDatasets)
		}
		return apiErr.Details
	}

	first := assertAmbiguousPaths(t, manifest.NewRelationshipGraph(relationships))
	second := assertAmbiguousPaths(t, manifest.NewRelationshipGraph([]*ossie.Relationship{
		relationships[3], relationships[2], relationships[1], relationships[0],
	}))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("candidate diagnostics depend on input order:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestRelationshipGraphDeduplicatesIdenticalDefinitions(t *testing.T) {
	first := relationship("a_to_b", "a", "b")
	duplicate := relationship("a_to_b", "a", "b")
	graph := manifest.NewRelationshipGraph([]*ossie.Relationship{first, duplicate})

	path, err := graph.ShortestPath("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(relationshipNames(path), []string{"a_to_b"}) {
		t.Fatalf("path = %#v", path)
	}
}

func TestRelationshipGraphReturnsTypedMissingPathError(t *testing.T) {
	graph := manifest.NewRelationshipGraph([]*ossie.Relationship{relationship("a_to_b", "a", "b")})
	_, err := graph.ShortestPath("a", "missing")
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrRelationshipNotFound {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrRelationshipNotFound)
	}
	if apiErr.Details["from"] != "a" || apiErr.Details["to"] != "missing" {
		t.Fatalf("details = %#v", apiErr.Details)
	}
}

func TestRelationshipGraphDirectedReachability(t *testing.T) {
	graph := manifest.NewRelationshipGraph([]*ossie.Relationship{
		relationship("orders_to_customer", "orders", "customer"),
		relationship("customer_to_region", "customer", "region"),
	})
	if !graph.CanReachDirected("orders", "region") {
		t.Fatal("orders should reach region in declared relationship direction")
	}
	if graph.CanReachDirected("region", "orders") {
		t.Fatal("directed reachability must not reverse relationship direction")
	}
}

func TestModelIndexResolvesDeterministicDatasetPathUnion(t *testing.T) {
	model := &manifest.ModelIndex{Graph: manifest.NewRelationshipGraph([]*ossie.Relationship{
		relationship("product_to_category", "product", "category"),
		relationship("orders_to_customer", "orders", "customer"),
		relationship("orders_to_product", "orders", "product"),
	})}
	plan, err := model.ResolveDatasetPaths("orders", []string{"category", "customer", "category"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.RequiredDatasets, []string{"category", "customer", "orders"}) {
		t.Fatalf("required datasets = %v", plan.RequiredDatasets)
	}
	if got := relationshipNames(plan.Relationships); !reflect.DeepEqual(got, []string{"orders_to_customer", "orders_to_product", "product_to_category"}) {
		t.Fatalf("relationship union = %v", got)
	}
	if len(plan.Paths) != 3 || plan.Paths[0].Dataset != "category" || !reflect.DeepEqual(relationshipNames(plan.Paths[0].Relationships), []string{"orders_to_product", "product_to_category"}) {
		t.Fatalf("dataset paths = %#v", plan.Paths)
	}
}

func relationship(name, from, to string) *ossie.Relationship {
	return &ossie.Relationship{
		Name:        name,
		From:        from,
		To:          to,
		FromColumns: []string{"id"},
		ToColumns:   []string{"id"},
	}
}

func relationshipNames(path []*ossie.Relationship) []string {
	out := make([]string, 0, len(path))
	for _, relationship := range path {
		out = append(out, relationship.Name)
	}
	return out
}

// A nil relationship graph is a structure Metis failed to build, not a
// relationship the model omitted. Reporting it as RELATIONSHIP_NOT_FOUND told
// the caller to add a relationship for a defect they cannot reach, and hid the
// defect behind a 404.
func TestNilRelationshipGraphReportsAMetisDefectNotAMissingRelationship(t *testing.T) {
	var graph *manifest.RelationshipGraph

	_, err := graph.ShortestPath("orders", "customers")

	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %#v, want *serrors.Error", err)
	}
	if apiErr.Code != serrors.ErrInternalInvariant {
		t.Fatalf("code = %s, want %s", apiErr.Code, serrors.ErrInternalInvariant)
	}
	if got := apiErr.Details["from"]; got != "orders" {
		t.Fatalf("details[from] = %#v, want orders", got)
	}
	if got := apiErr.Details["to"]; got != "customers" {
		t.Fatalf("details[to] = %#v, want customers", got)
	}
}

// A model whose relationship graph is present but has no path between two
// datasets is the caller-visible condition, and keeps its own code.
func TestMissingRelationshipPathStaysARelationshipCondition(t *testing.T) {
	graph := manifest.NewRelationshipGraph([]*ossie.Relationship{relationship("a_to_b", "a", "b")})

	_, err := graph.ShortestPath("a", "z")

	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrRelationshipNotFound {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrRelationshipNotFound)
	}
}
