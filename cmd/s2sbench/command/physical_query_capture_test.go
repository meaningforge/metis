package command

import (
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestPhysicalQueryCaptureCorrelatesAndSnapshotsByQueryID(t *testing.T) {
	captures := newPhysicalQueryCapture()
	compiled := &artifact.CompiledQuery{
		PhysicalQuery: sql.SQLRenderResult{Dialect: "DUCKDB", SQL: "SELECT ?", Parameters: []sql.QueryParameter{{Value: int64(7)}}},
		OutputSchema:  artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "value"}}},
	}
	if err := captures.record("query-7", compiled); err != nil {
		t.Fatal(err)
	}
	compiled.PhysicalQuery.SQL = "mutated"
	record, err := captures.decorateAttempt(readiness.AttemptRecord{QueryEvidence: &readiness.QueryEvidence{QueryID: "query-7"}})
	if err != nil {
		t.Fatal(err)
	}
	if record.ExecutedQuery == nil || record.ExecutedQuery.PhysicalQuery.SQL != "SELECT ?" || len(record.ExecutedQuery.PhysicalQuery.Parameters) != 1 {
		t.Fatalf("captured query = %#v", record.ExecutedQuery)
	}
	if _, err := captures.decorateAttempt(readiness.AttemptRecord{QueryEvidence: &readiness.QueryEvidence{QueryID: "query-7"}}); err == nil {
		t.Fatal("capture was not consumed exactly once")
	}
}
