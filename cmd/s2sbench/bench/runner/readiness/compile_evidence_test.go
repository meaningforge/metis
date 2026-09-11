package readiness

import (
	"context"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

type compileEvidenceExecution struct{}

func (compileEvidenceExecution) Prepare(context.Context, scenarios.Scenario) error { return nil }
func (compileEvidenceExecution) PhysicalSchema(context.Context) (string, error)    { return "", nil }
func (compileEvidenceExecution) RunSQL(context.Context, string, ...sql.QueryParameter) (scenarios.ResultSet, error) {
	return scenarios.ResultSet{
		Columns: []scenarios.ResultColumn{{Name: "value", ValueKind: scenarios.ResultInteger}},
		Rows:    []scenarios.ResultRow{{{ValueKind: scenarios.ResultInteger, Canonical: "1"}}},
	}, nil
}

func TestCompileEvidenceUsesStructuredResultWithoutAgentCopy(t *testing.T) {
	scenario := scenarios.Scenario{ExpectedResult: &scenarios.ResultExpectation{ResultSet: scenarios.ResultSet{
		Columns: []scenarios.ResultColumn{{Name: "value", ValueKind: scenarios.ResultInteger}},
		Rows:    []scenarios.ResultRow{{{ValueKind: scenarios.ResultInteger, Canonical: "1"}}},
	}, Comparison: scenarios.ResultUnordered}}
	response := `{"content":[{"type":"text","text":"{\"sql_render_result\":{\"dialect\":\"DUCKDB\",\"sql\":\"SELECT ? AS value\",\"parameters\":[{\"value\":1}]}}"}]}`
	records := finalizeCompileEvidence(context.Background(), compileEvidenceExecution{}, scenario, "DUCKDB", []AttemptRecord{{
		Output:    "compiled successfully\n{\"status\":\"ready\",\"dialect\":\"DUCKDB\",\"sql\":\"SELECT 1 AS value\"}",
		ToolTrace: []s2sbench.ToolCallEvidence{{Name: "compile_sql", Status: "success", Response: response}},
	}})
	if len(records) != 1 || records[0].CompileEvidence == nil || !records[0].CompileEvidence.ParseOK || !records[0].CompileEvidence.ExecutionOK || !records[0].CompileEvidence.OracleOK {
		t.Fatalf("compile evidence = %#v", records)
	}
	if !records[0].HandoffMatchesCompile {
		t.Fatal("harness did not accept the structured compile result directly")
	}
}
