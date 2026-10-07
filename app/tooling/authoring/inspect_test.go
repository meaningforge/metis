package authoring

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/clickhouse"
	"github.com/meaningforge/metis/renderer/doris"
)

type evidenceFactory struct {
	family   datasource.Type
	relation Relation
}

func (f evidenceFactory) DataSourceType() datasource.Type      { return f.family }
func (evidenceFactory) ValidateConfig(map[string]string) error { return nil }
func (f evidenceFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return evidenceRuntime{f.relation}, nil
}

type evidenceRuntime struct{ relation Relation }

func (r evidenceRuntime) Acquire(context.Context) (driver.Executor, error) {
	return evidenceLease{r.relation}, nil
}
func (evidenceRuntime) Close(context.Context) error { return nil }

type evidenceLease struct{ relation Relation }

func (evidenceLease) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	panic("inspection executed query")
}
func (evidenceLease) Close() error { return nil }
func (e evidenceLease) DescribeRelation(_ context.Context, ref driver.CatalogReference, _ driver.CatalogLimits) (driver.CatalogRelation, error) {
	m := driver.CatalogRelation{Reference: ref, Outcome: "found", ColumnsComplete: true}
	for i := len(e.relation.Columns) - 1; i >= 0; i-- {
		c := e.relation.Columns[i]
		n := c.NativeType
		m.Columns = append(m.Columns, driver.CatalogColumn{Name: c.Name, Nullable: c.Nullable, NativeType: driver.CatalogNativeType{Name: n.Name, Precision: n.Precision, Scale: n.Scale, Length: n.Length, Timezone: n.Timezone}})
	}
	return m, nil
}
func TestInspectSnapshotFeedsOfflineGeneration(t *testing.T) {
	for _, family := range []string{"doris", "clickhouse"} {
		t.Run(family, func(t *testing.T) {
			fixture, mapping := exampleInputs(t, family)
			relation := fixture.Relations[0]
			var render renderer.Renderer = doris.New()
			if family == "clickhouse" {
				render = clickhouse.New()
			}
			backends, err := backend.NewBackendRegistry(backend.Backend{Type: datasource.Type(family), Renderer: render, DriverFactory: evidenceFactory{datasource.Type(family), relation}})
			if err != nil {
				t.Fatal(err)
			}
			maxRows, maxBytes := int64(10000), int64(MaxCatalogBytes)
			sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{fixture.DataSource: {Type: datasource.Type(family), Config: map[string]string{}, Policy: datasource.DataSourcePolicy{QueryTimeout: "30s", MaxRows: &maxRows, MaxBytes: &maxBytes}}})
			if err != nil {
				t.Fatal(err)
			}
			execution := runner.New(sources, backends, nil, nil)
			defer execution.Close(context.Background())
			selectors := Selectors{SchemaVersion: 1, Relations: []driver.CatalogReference{{ID: relation.ID, Parts: relation.Parts}}}
			snapshot, err := InspectCatalog(context.Background(), execution, fixture.Project, fixture.DataSource, selectors)
			if err != nil {
				t.Fatal(err)
			}
			second, err := InspectCatalog(context.Background(), execution, fixture.Project, fixture.DataSource, selectors)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshot, second) {
				t.Fatal("unstable normalized evidence")
			}
			dir := t.TempDir()
			output := filepath.Join(dir, "catalog.json")
			if err := WriteSnapshot(context.Background(), output, snapshot); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadSnapshot(output)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Digest != snapshot.Digest {
				t.Fatal("digest changed")
			}
			candidate, err := Generate(context.Background(), loaded, mapping)
			if err != nil {
				t.Fatal(err)
			}
			if !candidate.Report.Validation.Valid {
				t.Fatal("invalid generated project")
			}
			if err := WriteCandidate(context.Background(), filepath.Join(dir, "candidate"), candidate); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(output)
			if err := WriteSnapshot(context.Background(), output, snapshot); err == nil {
				t.Fatal("replaced existing file")
			}
			after, _ := os.ReadFile(output)
			if !bytes.Equal(before, after) {
				t.Fatal("output changed")
			}
			stat, _ := os.Stat(output)
			if stat.Mode().Perm() != 0600 {
				t.Fatalf("permissions=%v", stat.Mode())
			}
			link := filepath.Join(dir, "link.json")
			if err := os.Symlink(output, link); err != nil {
				t.Fatal(err)
			}
			if err := WriteSnapshot(context.Background(), link, snapshot); err == nil {
				t.Fatal("replaced symlink")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := WriteSnapshot(ctx, filepath.Join(dir, "cancelled.json"), snapshot); err == nil {
				t.Fatal("published cancelled output")
			}
			leftovers, _ := filepath.Glob(filepath.Join(dir, ".metis-catalog-*"))
			if len(leftovers) != 0 {
				t.Fatalf("staging leftovers=%v", leftovers)
			}
		})
	}
}
func TestSelectorsAreStrictAndBounded(t *testing.T) {
	valid := []byte(`{"schema_version":1,"relations":[{"id":"orders","parts":["Analytics","Orders"]}]}`)
	if _, err := ParseSelectors(valid); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		`{"schema_version":1,"relations":[]}`,
		`{"schema_version":2,"relations":[{"id":"x","parts":["d","t"]}]}`,
		`{"schema_version":1,"relations":[{"id":"x","parts":["d","*"]}]}`,
		`{"schema_version":1,"relations":[{"id":"x","parts":["d","t"],"sql":"SECRET"}]}`,
		`{"schema_version":1,"relations":[{"id":"x","parts":["d","t"]},{"id":"y","parts":["d","t"]}]}`,
		`{"schema_version":1,"schema_version":1,"relations":[{"id":"x","parts":["d","t"]}]}`,
		"schema_version: 1\nrelations: &relations []\n",
		string(valid) + "\n---\nsecret: value",
	} {
		if _, err := ParseSelectors([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	large := Selectors{SchemaVersion: 1, Relations: make([]driver.CatalogReference, 201)}
	data, _ := json.Marshal(large)
	if _, err := ParseSelectors(data); err == nil {
		t.Fatal("accepted 201 selectors")
	}
	if _, err := ParseSelectors(make([]byte, MaxMappingBytes+1)); err == nil {
		t.Fatal("accepted oversized selectors")
	}
}

func TestSnapshotConcurrentPublicationNeverReplaces(t *testing.T) {
	snapshot, _ := exampleInputs(t, "doris")
	output := filepath.Join(t.TempDir(), "catalog.json")
	var winners atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if WriteSnapshot(context.Background(), output, snapshot) == nil {
				winners.Add(1)
			}
		}()
	}
	workers.Wait()
	if winners.Load() != 1 {
		t.Fatalf("successful publications=%d", winners.Load())
	}
	if loaded, err := LoadSnapshot(output); err != nil || loaded.Digest != snapshot.Digest {
		t.Fatalf("published catalog invalid: %v", err)
	}
}
