package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestLoadServeRuntimeWiresProductionDorisBackendIntoQueryMetrics(t *testing.T) {
	dir := t.TempDir()
	writeServeFile(t, filepath.Join(dir, "sales.ossie.yaml"), `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`)
	writeServeFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  sales:\n    path: ./sales.ossie.yaml\n")
	writeServeFile(t, filepath.Join(dir, "datasources.yaml"), `
doris-local:
  type: doris
  config:
    host: 127.0.0.1
    port: "1"
  policy:
    query_timeout: 1s
    max_rows: 10
    max_bytes: 1024
    max_concurrency: 8
`)
	configPath := filepath.Join(dir, "metis.yaml")
	writeServeFile(t, configPath, `
projects:
  analytics:
    path: ./project.yaml
    data_source: doris-local
data_sources:
  path: ./datasources.yaml
`)

	runtime, err := loadServeRuntime(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Execution == nil || runtime.QueryMetrics == nil || runtime.DimensionValues == nil || runtime.CompareMetrics == nil {
		t.Fatalf("serve runtime execution wiring = execution:%#v query_metrics:%#v dimension_values:%#v compare_metrics:%#v", runtime.Execution, runtime.QueryMetrics, runtime.DimensionValues, runtime.CompareMetrics)
	}
	resolved, err := runtime.Execution.ResolveDataSource("doris-local")
	if err != nil || resolved.Backend.Type != "doris" || resolved.Backend.Renderer.SQLDialect() != "DORIS" {
		t.Fatalf("production DataSource route = %#v, %v", resolved, err)
	}

	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	_, err = runtime.QueryMetrics.QueryMetrics(ctx, semantic.QueryMetricsRequest{Query: query.SemanticQuery{
		Project: "analytics", Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}},
	}})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrQueryExecutionFailed {
		t.Fatalf("production query execution error = %v", err)
	}
}

func TestLoadServeRuntimeKeepsCompileOnlyDeploymentResourceFree(t *testing.T) {
	dir := t.TempDir()
	writeServeFile(t, filepath.Join(dir, "sales.ossie.yaml"), `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
`)
	writeServeFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  sales:\n    path: ./sales.ossie.yaml\n")
	configPath := filepath.Join(dir, "metis.yaml")
	writeServeFile(t, configPath, "projects:\n  analytics:\n    path: ./project.yaml\n")

	runtime, err := loadServeRuntime(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Execution != nil || runtime.QueryMetrics == nil || runtime.DimensionValues == nil || runtime.CompareMetrics == nil {
		t.Fatalf("compile-only serve runtime = execution:%#v query_metrics:%#v dimension_values:%#v compare_metrics:%#v", runtime.Execution, runtime.QueryMetrics, runtime.DimensionValues, runtime.CompareMetrics)
	}
}

func writeServeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
