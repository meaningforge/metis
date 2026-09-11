package readiness

import (
	"encoding/json"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

func TestCapturedQueryMetricsIsTheMetisScoredResult(t *testing.T) {
	result := service.QueryMetricsResult{
		QueryID: "query-1",
		Schema:  artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "revenue", Datatype: ossie.DataTypeDecimal}}},
		Rows:    [][]any{{"42.00"}},
		Count:   1,
	}
	body, err := json.Marshal(map[string]any{"structuredContent": result})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := scenarios.ParseResultValue(scenarios.ResultNumber, "42")
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios.Scenario{ExpectedResult: &scenarios.ResultExpectation{
		ResultSet:  scenarios.ResultSet{Columns: []scenarios.ResultColumn{{Name: "revenue", ValueKind: scenarios.ResultNumber}}, Rows: []scenarios.ResultRow{{expected}}},
		Comparison: scenarios.ResultUnordered,
	}}
	record := AttemptRecord{Output: `{"status":"queried"}`, ToolTrace: []s2sbench.ToolCallEvidence{{Name: "metis.query_metrics", Status: "success", Response: string(body)}}}
	scored, observed := scoreCapturedQueryMetrics(scenario, record)
	if !observed || scored.Verdict != "correct" || scored.QueryEvidence == nil || !scored.QueryEvidence.OracleOK || scored.AnswerStatus != "queried" {
		t.Fatalf("query evidence = %#v", scored)
	}
}
