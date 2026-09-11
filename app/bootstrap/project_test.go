package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

const salesModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.public.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.region}]
            dimension: {}
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`

const customerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: customer
    datasets:
      - name: customers
        source: sales.public.customers
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customers.customer_id}]
    metrics:
      - name: customer_count
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: COUNT(customers.customer_id)}]
`

func TestLoadProjectRuntimeLoadsMultipleSemanticSourcesAndCompilesExplicitDialect(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "sales.ossie.yaml"), salesModel)
	mustWrite(t, filepath.Join(dir, "customer.ossie.yaml"), customerModel)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  models:
    path: ./*.ossie.yaml
`)

	runtime, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	projectID := filepath.Base(dir)
	project, err := runtime.SemanticManifest.Project(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Models) != 2 {
		t.Fatalf("models=%d want=2", len(project.Models))
	}
	if _, err := project.Model("sales"); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Model("customer"); err != nil {
		t.Fatal(err)
	}
	if runtime.SemanticGraph == nil || runtime.SemanticGraph.ManifestDigest() != runtime.SemanticManifest.Digest {
		t.Fatalf("runtime semantic graph is not built for manifest %q", runtime.SemanticManifest.Digest)
	}
	projectGraph, err := runtime.SemanticGraph.Project(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if got := projectGraph.ModelNames(); !reflect.DeepEqual(got, []string{"customer", "sales"}) {
		t.Fatalf("graph models from semantic sources = %v", got)
	}

	result, err := runtime.Compile.Compile(context.Background(), service.CompileRequest{Query: query.SemanticQuery{
		Project:    projectID,
		Model:      "sales",
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "region"}},
	}, Dialect: "DORIS"})
	if err != nil {
		t.Fatal(err)
	}
	physicalQuery := result.SQLRenderResult
	if physicalQuery.Dialect != "DORIS" {
		t.Fatalf("physical query dialect=%q want DORIS", physicalQuery.Dialect)
	}
	if !strings.Contains(physicalQuery.SQL, "SUM(orders.amount)") || !strings.Contains(physicalQuery.SQL, "GROUP BY") {
		t.Fatalf("unexpected SQL: %s", physicalQuery.SQL)
	}
	if len(result.OutputSchema.Columns) != 2 {
		t.Fatalf("output schema: %#v", result.OutputSchema)
	}
	if got := result.OutputSchema.Columns[0]; got.Name != "region" || got.Kind != artifact.OutputDimension || got.Datatype != ossie.DataTypeString {
		t.Fatalf("dimension output: %#v", got)
	}
	if got := result.OutputSchema.Columns[1]; got.Name != "total_revenue" || got.Kind != artifact.OutputMetric || got.Datatype != ossie.DataTypeDecimal {
		t.Fatalf("metric output: %#v", got)
	}
}

func TestLoadProjectRuntimeRejectsGlobWithNoMatches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  models:
    path: ./models/*.ossie.yaml
`)
	_, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err == nil || !strings.Contains(err.Error(), "matched no files") {
		t.Fatalf("expected no-match glob error, got %v", err)
	}
}

func TestLoadProjectRuntimeRejectsDuplicateSemanticModelsAcrossSources(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "sales.ossie.yaml"), salesModel)
	mustWrite(t, filepath.Join(dir, "duplicate.ossie.yaml"), salesModel)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
  duplicate:
    path: ./duplicate.ossie.yaml
`)
	_, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err == nil || !strings.Contains(err.Error(), `duplicate SemanticModel "sales"`) {
		t.Fatalf("expected duplicate semantic model error, got %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
