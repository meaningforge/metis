package regression

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const runtimeSuite = `schema_version: 1
project: demo
fixture: {id: demo-v1, kind: externally_prepared}
cases:
  - id: revenue
    operation: query_metrics
    request:
      query:
        project: demo
        model: sales
        metrics: [{name: total_revenue}]
    expect:
      outcome: success
      output_schema:
        columns: [{name: total_revenue, kind: metric, datatype: Decimal}]
      row_count: 1
      rows:
        mode: unordered
        values: [[{type: decimal, value: "9007199254740993.01"}]]
`

func TestRuntimeSuiteStrictValidation(t *testing.T) {
	if _, _, err := ParseSuite([]byte(runtimeSuite)); err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{
		{"kind: externally_prepared", "kind: managed"},
		{"operation: query_metrics", "operation: compare_metrics"},
		{"mode: unordered", "mode: unordered\n        tolerances: {total_revenue: {abs: '0.1'}}"},
		{"row_count: 1", "row_count: 1001"},
		{"value: \"9007199254740993.01\"", "value: 9007199254740993.01"},
		{"type: decimal", "type: float"},
		{"mode: unordered", "mode: random"},
		{"mode: unordered", "mode: unordered\n        tolerances: {total_revenue: {abs: '-1'}}"},
		{"model: sales", "model: sales\n        sql: SELECT 1"},
	} {
		if _, _, err := ParseSuite([]byte(strings.Replace(runtimeSuite, change[0], change[1], 1))); err == nil {
			t.Errorf("accepted %q", change[1])
		}
	}
	suite, _, _ := ParseSuite([]byte(runtimeSuite))
	suite.Cases[0].Expect = Expectation{Outcome: "semantic_error", Code: "QUERY_EXECUTION_FAILED", CallerAction: "CHANGE_TARGET"}
	if err := suite.validate(); err == nil {
		t.Fatal("accepted execution failure expectation")
	}
}

func TestEvaluateRuntimePrecisionFailuresAndPrivacy(t *testing.T) {
	suite, _, err := ParseSuite([]byte(runtimeSuite))
	if err != nil {
		t.Fatal(err)
	}
	c := suite.Cases[0]
	result := &semantic.QueryMetricsResult{Schema: artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "total_revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal}}}, Count: 1, Rows: [][]any{{"9007199254740993.0100"}}}
	if got := evaluateRuntime(c, result, nil, nil); got.Status != "passed" {
		t.Fatalf("equal decimal: %#v", got)
	}
	result.Rows[0][0] = "9007199254740993.02"
	got := evaluateRuntime(c, result, nil, nil)
	if got.Status != "failed" || got.Category != "runtime_assertion_mismatch" {
		t.Fatalf("precision: %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "9007199254740993") {
		t.Fatal("report leaked row values")
	}
	result.Rows[0] = nil
	if got := evaluateRuntime(c, result, nil, nil); got.Category != "invalid_result_shape" {
		t.Fatalf("shape: %#v", got)
	}
	for _, failure := range []error{context.Canceled, &serrors.Error{Code: serrors.ErrQueryExecutionFailed, Message: "secret driver details"}} {
		if got := evaluateRuntime(c, nil, failure, nil); got.Status != "failed" || got.ActualOutcome != "incomplete" {
			t.Fatalf("failure: %#v", got)
		}
	}
	c.Expect = Expectation{Outcome: "semantic_error", Code: "METRIC_NOT_FOUND", CallerAction: "CHANGE_REQUEST"}
	if got := evaluateRuntime(c, nil, &serrors.Error{Code: serrors.ErrMetricNotFound}, nil); got.Status != "passed" {
		t.Fatalf("semantic error: %#v", got)
	}
}

func TestRuntimeLoadFailureIsNotRun(t *testing.T) {
	suite, digest, _ := ParseSuite([]byte(runtimeSuite))
	backends, err := backend.NewBackendRegistry()
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunRuntime(context.Background(), suite, digest, RuntimeOptions{Project: "demo", Config: filepath.Join(t.TempDir(), "missing.yaml"), Backends: backends})
	if err != nil || report.NotRun != 1 || report.Status != "failed" {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if report.FixtureVerification != "declared_only" {
		t.Fatal("fixture claims verification")
	}
}

func TestJUnitCountsPrivacyAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.xml")
	report := Report{Mode: "runtime", Cases: []CaseReport{{ID: "ok", Status: "passed"}, {ID: "mismatch", Status: "failed", Category: "runtime_assertion_mismatch"}, {ID: "a<&", Status: "not_run", Category: "runtime_load_failed"}}}
	if err := WriteJUnitReport(path, report, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var parsed struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Errors   int `xml:"errors,attr"`
	}
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Tests != 3 || parsed.Failures != 1 || parsed.Errors != 1 {
		t.Fatalf("counts: %#v", parsed)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("not private")
	}
	if err := WriteJUnitReport(path, report, false); err == nil {
		t.Fatal("overwrote report")
	}
}
