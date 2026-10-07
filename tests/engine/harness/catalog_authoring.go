package harness

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/tooling/authoring"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
)

// RunCatalogAuthoringContract verifies real metadata through the production
// pool/lease and feeds the result into the shipped offline generator.
func RunCatalogAuthoringContract(t *testing.T, execution *runner.Runner, source string) {
	t.Helper()
	selectors := authoring.Selectors{SchemaVersion: 1, Relations: []driver.CatalogReference{{ID: "events", Parts: []string{"analytics", "workflow_events"}}}}
	snapshot, err := authoring.InspectCatalog(context.Background(), execution, "analytics", source, selectors)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Relations) != 1 || len(snapshot.Relations[0].Columns) != 5 {
		t.Fatalf("incomplete inventory=%#v", snapshot.Relations)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	if err := authoring.WriteSnapshot(context.Background(), path, snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := authoring.LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	mapping := authoring.Mapping{SchemaVersion: 1, Project: "analytics", Model: "observed", DataSource: source, Datasets: []authoring.DatasetMapping{{Relation: "events", Name: "events", Fields: []authoring.FieldMapping{{Column: "amount", Name: "amount"}, {Column: "region", Name: "region", Dimension: true}, {Column: "event_time", Name: "event_time"}}}}, StarterMetrics: []authoring.StarterMetric{{Name: "event_rows", Kind: "row_count", Dataset: "events"}}}
	candidate, err := authoring.Generate(context.Background(), loaded, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := authoring.WriteCandidate(context.Background(), filepath.Join(dir, "candidate"), candidate); err != nil {
		t.Fatal(err)
	}
	selectors.Relations[0].Parts[1] = "metis_absent_catalog_relation"
	if partial, err := authoring.InspectCatalog(context.Background(), execution, "analytics", source, selectors); err == nil || len(partial.Relations) != 0 {
		t.Fatalf("absent relation produced evidence=%#v err=%v", partial, err)
	}
}
