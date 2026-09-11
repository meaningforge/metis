package baseline

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/evidence"
)

// The parity check is the archives' own coverage contract, so it needs to fail
// for the reason it exists rather than merely return nil today. Both directions
// are exercised: a renderer the archives would skip, and an evidence row for a
// renderer that does not exist.
func TestParityCheckFailsClosedInBothDirections(t *testing.T) {
	targets := []evidence.Target{{Dialect: "DORIS"}, {Dialect: "CLICKHOUSE"}}

	if err := requireEvidenceCoversEveryRenderer(targets, []string{"doris", "clickhouse"}); err != nil {
		t.Fatalf("matching sets reported a mismatch: %v", err)
	}

	err := requireEvidenceCoversEveryRenderer(targets, []string{"doris", "clickhouse", "duckdb"})
	if err == nil || !strings.Contains(err.Error(), "duckdb") {
		t.Errorf("a registered renderer with no evidence target was not reported: %v", err)
	}

	err = requireEvidenceCoversEveryRenderer(targets, []string{"doris"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "clickhouse") {
		t.Errorf("an evidence target with no registered renderer was not reported: %v", err)
	}

	err = requireEvidenceCoversEveryRenderer(targets, []string{"doris", "duckdb"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "duckdb") || !strings.Contains(strings.ToLower(err.Error()), "clickhouse") {
		t.Errorf("a mismatch in both directions was not fully reported: %v", err)
	}
}
