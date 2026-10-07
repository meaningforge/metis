package authoring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func seal(t *testing.T, s Snapshot) Snapshot {
	t.Helper()
	var err error
	s.Digest, err = CatalogDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func exampleInputs(t *testing.T, backend string) (Snapshot, Mapping) {
	t.Helper()
	precision, scale := 18, 2
	s := Snapshot{SchemaVersion: 1, Project: "sales", DataSource: "warehouse", Backend: backend, Relations: []Relation{{ID: "orders", Parts: []string{"Sales", "Order Items"}, Outcome: "found", ColumnsComplete: true, Columns: []Column{
		{Name: "Region", NativeType: NativeType{Name: "String"}, Nullable: "nullable"},
		{Name: "Order ID", NativeType: NativeType{Name: "BIGINT"}, Nullable: "nullable"},
		{Name: "Amount", NativeType: NativeType{Name: "Decimal", Precision: &precision, Scale: &scale}, Nullable: "not_null"},
		{Name: "private_column", NativeType: NativeType{Name: "unsupported_native"}, Nullable: "unknown"},
	}, Keys: []Key{{Kind: "sorting", Columns: []string{"Region"}}}}}}
	if backend == "clickhouse" {
		s.Relations[0].Columns[1].NativeType.Name = "Int64"
	}
	if backend == "duckdb" {
		s.Relations[0].Columns[0].NativeType.Name = "VARCHAR"
	}
	m := Mapping{SchemaVersion: 1, Project: "sales", Model: "sales", Datasets: []DatasetMapping{{Relation: "orders", Name: "orders", Fields: []FieldMapping{{Column: "Region", Name: "region", Dimension: true}, {Column: "Order ID", Name: "order_id"}, {Column: "Amount", Name: "amount"}}}}, StarterMetrics: []StarterMetric{{Name: "order_rows", Kind: "row_count", Dataset: "orders"}}}
	return seal(t, s), m
}

func TestGeneratedCandidateLoadsAndRendersExactPhysicalIdentity(t *testing.T) {
	for _, backend := range []string{"doris", "clickhouse", "duckdb"} {
		t.Run(backend, func(t *testing.T) {
			s, m := exampleInputs(t, backend)
			candidate, err := Generate(context.Background(), s, m)
			if err != nil {
				t.Fatal(err)
			}
			guide := string(candidate.Files["GETTING_STARTED.md"])
			if !strings.Contains(guide, "metis catalog inspect") || !strings.Contains(guide, "generation report is not approval") || strings.Contains(guide, "Online catalog inspection and runtime validation are follow-up tooling proposals") {
				t.Fatal("generated guide misrepresents shipped inspection or manual review")
			}
			model := candidate.Files["models/sales.ossie.yaml"]
			if bytes.Contains(model, []byte("private_column")) || bytes.Contains(model, []byte("unique_keys")) || bytes.Contains(model, []byte("primary_key")) {
				t.Fatal("invented or unselected semantics")
			}
			doc, err := ossie.NewLoader().Load(model)
			if err != nil {
				t.Fatal(err)
			}
			placement, ok, err := ossie.SemanticModelDataSource(&doc.SemanticModel[0])
			if err != nil || !ok || placement.Name != "warehouse" {
				t.Fatalf("placement=%#v %v", placement, err)
			}
			cfg, err := execution.LoadProjectConfig(candidate.Files["project.yaml"])
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := bootstrap.NewRuntime(context.Background(), bootstrap.RuntimeInput{Projects: map[string]bootstrap.ProjectInput{"sales": {Config: cfg, Documents: []source.SourceDocument{{Source: "sales", Path: "models/sales.ossie.yaml", Content: model}}}}}, bootstrap.WithLocalAllAccessProjectAuthorization())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close(context.Background())
			compiled, err := runtime.Compile.Compile(context.Background(), semantic.CompileRequest{Query: query.SemanticQuery{Project: "sales", Model: "sales", Metrics: []query.MetricRef{{Name: "order_rows"}}, Dimensions: []query.DimensionRef{{Name: "region"}}}, Dialect: queryDialect(backend)})
			if err != nil {
				t.Fatal(err)
			}
			physical, column := "`Sales`.`Order Items`", "`Region`"
			if backend == "duckdb" {
				physical, column = `"Sales"."Order Items"`, `"Region"`
			}
			for _, value := range []string{physical, column, "COUNT(*)"} {
				if !strings.Contains(compiled.SqlRenderResult.SQL, value) {
					t.Errorf("missing physical identifier %s: %s", value, compiled.SqlRenderResult.SQL)
				}
			}
			if len(compiled.OutputSchema.Columns) != 2 || compiled.OutputSchema.Columns[1].Datatype != ossie.DataTypeInteger {
				t.Fatalf("schema=%#v", compiled.OutputSchema)
			}
			countOnly, err := runtime.Compile.Compile(context.Background(), semantic.CompileRequest{Query: query.SemanticQuery{Project: "sales", Model: "sales", Metrics: []query.MetricRef{{Name: "order_rows"}}}, Dialect: queryDialect(backend)})
			if err != nil || !strings.Contains(countOnly.SqlRenderResult.SQL, physical) {
				t.Fatalf("unambiguous count binding: %#v %v", countOnly, err)
			}
			if !candidate.Report.Validation.Valid || len(candidate.Report.ReviewTasks) == 0 {
				t.Fatal("missing validation/review evidence")
			}
			out := filepath.Join(t.TempDir(), "candidate")
			if err := WriteCandidate(context.Background(), out, candidate); err != nil {
				t.Fatal(err)
			}
			result := source.ValidateProject("sales", filepath.Join(out, "project.yaml"))
			if !result.Valid {
				t.Fatalf("validation=%#v", result)
			}
			data, _ := os.ReadFile(filepath.Join(out, "authoring-report.json"))
			if bytes.Contains(data, []byte(out)) || bytes.Contains(data, []byte(".metis-authoring-")) {
				t.Fatal("nondeterministic output path in report")
			}
		})
	}
}

func TestReservedPhysicalNamesStayQuotedAndMultiDatasetCountsFail(t *testing.T) {
	for _, backend := range []string{"doris", "clickhouse"} {
		s, m := exampleInputs(t, backend)
		s.Relations[0].Parts = []string{"Sales", "select"}
		s.Relations[0].Columns[0].Name = "from"
		s.Relations[0].Keys = nil
		m.Datasets[0].Fields[0].Column = "from"
		s = seal(t, s)
		c, err := Generate(context.Background(), s, m)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(c.Files["models/sales.ossie.yaml"], []byte("orders.`from`")) {
			t.Fatal("lost reserved physical column quoting")
		}
		second := s.Relations[0]
		second.ID = "other"
		second.Parts = []string{"Sales", "other"}
		s.Relations = append(s.Relations, second)
		s = seal(t, s)
		m.Datasets = append(m.Datasets, DatasetMapping{Relation: "other", Name: "other", Fields: []FieldMapping{{Column: "from", Name: "source_field"}}})
		if _, err := Generate(context.Background(), s, m); err == nil {
			t.Fatal("count binding silently picked one of several datasets")
		}
		m.StarterMetrics = nil
		if _, err := Generate(context.Background(), s, m); err != nil {
			t.Fatal("metric-free multi-dataset candidate:", err)
		}
	}
}

func TestGenerationIsDeterministicAndOwnsInputs(t *testing.T) {
	s, m := exampleInputs(t, "doris")
	original, _ := json.Marshal(s)
	a, err := Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(s)
	if !bytes.Equal(data, original) {
		t.Fatal("mutated input")
	}
	s.Relations[0].Columns[0], s.Relations[0].Columns[2] = s.Relations[0].Columns[2], s.Relations[0].Columns[0]
	s.Observation = &Observation{ObservedAt: "2026-10-07T00:00:00Z", ServerVersion: "unverified-observation"}
	m.Datasets[0].Fields[0], m.Datasets[0].Fields[2] = m.Datasets[0].Fields[2], m.Datasets[0].Fields[0]
	b, err := Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Files, b.Files) {
		t.Fatal("inventory/map reordering or observation changed generated files")
	}
	before, _ := json.Marshal(a.Report)
	*s.Relations[0].Columns[0].NativeType.Precision = 19
	if after, _ := json.Marshal(a.Report); !bytes.Equal(before, after) {
		t.Fatal("caller mutation changed returned evidence")
	}
	for _, c := range []Candidate{a, b} {
		out := filepath.Join(t.TempDir(), "different-name")
		if err := WriteCandidate(context.Background(), out, c); err != nil {
			t.Fatal(err)
		}
		for path, want := range a.Files {
			got, err := os.ReadFile(filepath.Join(out, path))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("changed %s", path)
			}
		}
	}
}

func TestGeneratorFailsClosedOnUnsupportedSelections(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*Snapshot, *Mapping)
	}{
		{"project", "AUTHORING_SOURCE_MISMATCH", func(s *Snapshot, m *Mapping) { m.Project = "other" }},
		{"source", "AUTHORING_SOURCE_MISMATCH", func(s *Snapshot, m *Mapping) { m.DataSource = "other" }},
		{"relation", "AUTHORING_RELATION_NOT_FOUND", func(s *Snapshot, m *Mapping) { m.Datasets[0].Relation = "unknown" }},
		{"column", "AUTHORING_COLUMN_NOT_FOUND", func(s *Snapshot, m *Mapping) { m.Datasets[0].Fields[0].Column = "unknown" }},
		{"type", "AUTHORING_UNSUPPORTED_TYPE", func(s *Snapshot, m *Mapping) { m.Datasets[0].Fields[0].Column = "private_column" }},
		{"reserved", "AUTHORING_INVALID_NAME", func(s *Snapshot, m *Mapping) { m.Datasets[0].Name = "select" }},
		{"collision", "AUTHORING_INVALID_MAPPING", func(s *Snapshot, m *Mapping) { m.Datasets[0].Fields[1].Name = "REGION" }},
		{"dot", "AUTHORING_UNSUPPORTED_IDENTIFIER", func(s *Snapshot, m *Mapping) { s.Relations[0].Parts[1] = "other.orders" }},
		{"prequoted", "AUTHORING_UNSUPPORTED_IDENTIFIER", func(s *Snapshot, m *Mapping) { s.Relations[0].Parts[1] = "`orders`" }},
		{"quoted column", "AUTHORING_UNSUPPORTED_IDENTIFIER", func(s *Snapshot, m *Mapping) {
			s.Relations[0].Columns[0].Name = "a\"b"
			s.Relations[0].Keys = nil
			m.Datasets[0].Fields[0].Column = "a\"b"
		}},
		{"partial", "AUTHORING_CATALOG_INCOMPLETE", func(s *Snapshot, m *Mapping) { s.Relations[0].ColumnsComplete = false }},
		{"metric", "AUTHORING_INVALID_MAPPING", func(s *Snapshot, m *Mapping) { m.StarterMetrics[0].Kind = "sum" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, m := exampleInputs(t, "doris")
			tc.change(&s, &m)
			s = seal(t, s)
			_, err := Generate(context.Background(), s, m)
			var f *Finding
			if !errors.As(err, &f) || f.Code != tc.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestMetricFreeSkeletonAndKnownNullability(t *testing.T) {
	s, m := exampleInputs(t, "clickhouse")
	m.StarterMetrics = nil
	s.Relations[0].Columns[0].Nullable = "unknown"
	s = seal(t, s)
	c, err := Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ossie.NewLoader().Load(c.Files["models/sales.ossie.yaml"])
	if err != nil || len(doc.SemanticModel[0].Metrics) != 0 {
		t.Fatalf("doc=%#v err=%v", doc, err)
	}
	if !strings.Contains(strings.Join(c.Report.ReviewTasks, " "), "No metrics") {
		t.Fatal("missing business review warning")
	}
}

func TestProjectIdentityAndInstructionsPreserveExistingNamespace(t *testing.T) {
	s, m := exampleInputs(t, "doris")
	s.Project = "tenant-123's-project"
	m.Project = s.Project
	s = seal(t, s)
	c, err := Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	if c.Report.Project != s.Project || !bytes.Contains(c.Files["GETTING_STARTED.md"], []byte("--project 'tenant-123'\\''s-project'")) {
		t.Fatal("changed Project identity or unsafe command quoting")
	}
}
