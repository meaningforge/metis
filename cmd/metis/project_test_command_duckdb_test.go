//go:build duckdb

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/tests/engine/duckdb/fixture"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

func TestProjectRuntimeSuiteThroughProductionDuckDB(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "fixture.duckdb")
	fixtureBackend, err := fixture.New(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtureBackend.PrepareFixture(context.Background(), enginefixture.AnalyticsWorkflows); err != nil {
		t.Fatal(err)
	}
	if err := fixtureBackend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeServeFile(t, filepath.Join(dir, "model.yaml"), enginefixture.AnalyticsModelYAML)
	writeServeFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  analytics: {path: ./model.yaml}\n")
	writeServeFile(t, filepath.Join(dir, "datasources.yaml"), "local:\n  type: duckdb\n  config:\n    path: "+databasePath+"\n  policy:\n    query_timeout: 5s\n    max_rows: 10\n    max_bytes: 1024\n")
	config := filepath.Join(dir, "metis.yaml")
	writeServeFile(t, config, "projects:\n  analytics: {path: ./project.yaml, data_source: local}\ndata_sources: {path: ./datasources.yaml}\n")
	suite := filepath.Join(dir, "suite.yaml")
	input := `schema_version: 1
project: analytics
fixture: {id: analytics-v1, kind: externally_prepared}
cases:
  - id: revenue
    operation: query_metrics
    request:
      query:
        project: analytics
        model: workflow
        metrics: [{name: total_revenue}]
    expect:
      outcome: success
      output_schema:
        columns: [{name: total_revenue, kind: metric, datatype: Decimal}]
      row_count: 1
      rows:
        mode: unordered
        values: [[{type: decimal, value: "12.5"}]]
`
	writeServeFile(t, suite, input)
	output, junit := filepath.Join(dir, "report.json"), filepath.Join(dir, "report.xml")
	args := []string{"--mode", "runtime", "--project", "analytics", "--config", config, "--suite", suite, "--output", output, "--junit-output", junit}
	if code := testProject(args); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(output)
	var report regression.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Passed != 1 || report.CandidateDigest == "" || len(report.Backends) != 1 || report.Backends[0] != "duckdb" {
		t.Fatalf("report: %#v", report)
	}
	if strings.Contains(string(data), "12.5") || strings.Contains(string(data), databasePath) {
		t.Fatal("report leaked result or endpoint")
	}
	writeServeFile(t, suite, strings.Replace(input, `value: "12.5"`, `value: "12.6"`, 1))
	if code := testProject(append(args, "--overwrite")); code != 1 {
		t.Fatalf("mismatch exit %d", code)
	}
	writeServeFile(t, suite, strings.Replace(input, "total_revenue", "missing_metric", 1))
	if code := testProject(append(args, "--overwrite")); code != 1 {
		t.Fatalf("semantic failure exit %d", code)
	}
	errorInput := strings.Replace(input, "total_revenue", "missing_metric", 1)
	errorInput = strings.Split(errorInput, "    expect:")[0] + "    expect: {outcome: semantic_error, code: METRIC_NOT_FOUND, caller_action: CHANGE_REQUEST}\n"
	writeServeFile(t, suite, errorInput)
	if code := testProject(append(args, "--overwrite")); code != 0 {
		t.Fatalf("expected semantic error exit %d", code)
	}
	writeServeFile(t, suite, input)
	writeServeFile(t, filepath.Join(dir, "datasources.yaml"), "local:\n  type: duckdb\n  config:\n    path: "+databasePath+"\n  policy:\n    query_timeout: 5s\n    max_rows: 10\n    max_bytes: 1\n")
	if code := testProject(append(args, "--overwrite")); code != 1 {
		t.Fatalf("byte limit exit %d", code)
	}
	data, _ = os.ReadFile(output)
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Cases[0].ActualOutcome != "incomplete" {
		t.Fatalf("limit report: %#v", report)
	}
}
