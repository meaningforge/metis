//go:build duckdb

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/query"
	duckdbfixture "github.com/meaningforge/metis/tests/engine/duckdb/fixture"
	"github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestLoadServeRuntimeExecutesAnalyticsWorkflowsThroughProductionDuckDBBackend(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "metis.duckdb")
	fixtureBackend, err := duckdbfixture.New(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtureBackend.PrepareFixture(context.Background(), fixture.AnalyticsWorkflows); err != nil {
		t.Fatal(err)
	}
	if err := fixtureBackend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeServeFile(t, filepath.Join(dir, "analytics.ossie.yaml"), fixture.AnalyticsModelYAML)
	writeServeFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  analytics:\n    path: ./analytics.ossie.yaml\n")
	writeServeFile(t, filepath.Join(dir, "datasources.yaml"), `
duckdb-local:
  type: duckdb
  config:
    path: `+databasePath+`
  policy:
    query_timeout: 5s
    max_rows: 10
    max_bytes: 1024
    max_concurrency: 4
`)
	configPath := filepath.Join(dir, "metis.yaml")
	writeServeFile(t, configPath, `
projects:
  analytics:
    path: ./project.yaml
    data_source: duckdb-local
data_sources:
  path: ./datasources.yaml
`)

	runtime, err := loadServeRuntime(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := runtime.Execution.Close(context.Background()); closeErr != nil {
			t.Errorf("close runtime: %v", closeErr)
		}
	})
	resolved, err := runtime.Execution.ResolveDataSource("duckdb-local")
	if err != nil || resolved.Backend.Type != "duckdb" || resolved.Backend.Renderer.SQLDialect() != "DUCKDB" {
		t.Fatalf("production DuckDB route = %#v, %v", resolved, err)
	}
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := runtime.QueryMetrics.QueryMetrics(ctx, semantic.QueryMetricsRequest{Query: query.SemanticQuery{
		Project: fixture.AnalyticsProject, Model: fixture.AnalyticsModel, Metrics: []query.MetricRef{{Name: "total_revenue"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0] != "12.5" {
		t.Fatalf("query_metrics DuckDB result = %#v", result)
	}
	values, err := runtime.DimensionValues.GetDimensionValues(ctx, semantic.DimensionValuesQuery{
		ProjectID: fixture.AnalyticsProject,
		Dimension: "dimension:workflow.events.region",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values.QueryID == "" || values.DataType != "String" || values.Count != 1 || len(values.Values) != 1 || values.Values[0] != "west" || values.Truncated {
		t.Fatalf("get_dimension_values DuckDB result = %#v", values)
	}
	harness.RunAnalyticsWorkflowContract(t, harness.AnalyticsWorkflowRuntime{
		Name:            "DUCKDB",
		AttributeMetric: runtime.AttributeMetric.AttributeMetric,
		CompareMetrics:  runtime.CompareMetrics.CompareMetrics,
	})
}
