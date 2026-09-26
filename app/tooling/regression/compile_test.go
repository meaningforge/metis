package regression

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestRunCompileUsesOfflineProjectServices(t *testing.T) {
	suite, digest, err := ParseSuite([]byte(validSuite))
	if err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Project: "demo", Config: "../../../examples/demo/project.yaml", Dialect: sql.SQLDialect("DORIS")}
	report, err := RunCompile(context.Background(), suite, digest, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || report.Passed != 1 || report.Failed != 0 || report.NotRun != 0 || report.CandidateDigest == "" {
		t.Fatalf("report = %#v", report)
	}

	suite.Cases[0].Expect.OutputSchema.Columns[0].Datatype = "Integer"
	report, err = RunCompile(context.Background(), suite, digest, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "failed" || report.Cases[0].Category != "compile_assertion_mismatch" || report.Cases[0].ActualSchema == nil {
		t.Fatalf("mismatch report = %#v", report)
	}
	if report.Cases[0].ActualSchema.Columns[0].Datatype != "String" {
		t.Fatalf("actual schema = %#v", report.Cases[0].ActualSchema)
	}
}

func TestRunCompileMatchesStableSemanticError(t *testing.T) {
	input := strings.Replace(validSuite, "total_revenue", "missing_metric", 1)
	input = strings.Replace(input, "outcome: success\n      output_schema:\n        columns:\n          - {name: region, kind: dimension, datatype: String}\n          - {name: total_revenue, kind: metric, datatype: Decimal}\n      warnings: []", "outcome: semantic_error\n      code: METRIC_NOT_FOUND\n      caller_action: CHANGE_REQUEST", 1)
	suite, digest, err := ParseSuite([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunCompile(context.Background(), suite, digest, CompileOptions{Project: "demo", Config: "../../../examples/demo/project.yaml", Dialect: "DORIS"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || report.Cases[0].Code != "METRIC_NOT_FOUND" {
		t.Fatalf("error report = %#v", report)
	}
}

func TestRunCompileSourceFailureDoesNotPassCases(t *testing.T) {
	suite, digest, err := ParseSuite([]byte(validSuite))
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunCompile(context.Background(), suite, digest, CompileOptions{Project: "demo", Config: filepath.Join(t.TempDir(), "missing.yaml"), Dialect: "DORIS"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "failed" || report.NotRun != 1 || report.Cases[0].Category != "candidate_load_failed" {
		t.Fatalf("source failure report = %#v", report)
	}
}

func TestCompileSnapshotAssertionsAreExactAndReportNoSQL(t *testing.T) {
	grain := query.TimeGrainMonth
	caseInput := Case{ID: "snapshot", Expect: Expectation{
		Outcome:         "success",
		OutputSchema:    &ExpectedSchema{Columns: []ExpectedColumn{{Name: "month", Kind: "dimension", Datatype: "DateTime", Grain: &grain}}},
		SQLRenderResult: &ExpectedSQL{CompilerVersion: "dev", Dialect: "DORIS", SQL: "SELECT ?", Parameters: []ExpectedParameter{{Type: "decimal", Value: "1.25"}}},
	}}
	compiled := &artifact.CompiledQuery{
		OutputSchema:    artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "month", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeDateTime, Grain: &grain}}},
		SqlRenderResult: sql.SqlRenderResult{Dialect: "DORIS", SQL: "SELECT ?", Parameters: []sql.QueryParameter{{Value: json.Number("1.25")}}},
	}
	result := evaluateCompile(caseInput, compiled, nil, nil)
	if result.Status != "passed" {
		t.Fatalf("matching snapshot = %#v", result)
	}
	caseInput.Expect.OutputSchema.Columns[0].Grain = nil
	caseInput.Expect.SQLRenderResult.SQL = "SELECT 1"
	result = evaluateCompile(caseInput, compiled, nil, nil)
	if result.Status != "failed" || len(result.Differences) != 2 {
		t.Fatalf("snapshot mismatch = %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SELECT") || strings.Contains(string(encoded), "1.25") {
		t.Fatalf("report leaked SQL or parameter: %s", encoded)
	}
}

func TestWriteReportIsPrivateAndRefusesExistingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	report := Report{SchemaVersion: 1, Mode: "compile", Status: "failed"}
	if err := WriteReport(path, report, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("report permissions = %o", info.Mode().Perm())
	}
	if err := WriteReport(path, Report{Status: "passed"}, false); err == nil {
		t.Fatal("overwrote report without --overwrite")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Report
	if err := json.Unmarshal(data, &saved); err != nil || saved.Status != "failed" {
		t.Fatalf("preserved report = %#v, %v", saved, err)
	}
	if err := WriteReport(path, Report{Status: "passed"}, true); err != nil {
		t.Fatal(err)
	}
}
