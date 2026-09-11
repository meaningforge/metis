package doris_test

import (
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestRatioAttributionExecutionEvidence(t *testing.T) {
	dsn := dorisDSN(t)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("connect to Doris: %v", err)
	}
	waitForBackend(t, db)
	prepareFixture(t, db, enginefixture.RatioAttribution)
	execution := openProductionExecution(t)
	queries := harness.CompileRatioAttributionBundleForExecution(t, execution, []string{"segment", "channel"}, true)
	if len(queries) != 2 || queries[0].Dimension != "channel" || queries[1].Dimension != "segment" {
		t.Fatalf("ratio attribution bundle order = %#v", queries)
	}
	harness.RunDefinedRatioAttributionBundleEvidence(t, execution, queries)
	segment := queries[1].CompiledQuery
	execution.Close(t)

	prepareFixture(t, db, enginefixture.RatioAttributionUndefinedSegment)
	execution = openProductionExecution(t)
	harness.RunUndefinedRatioSegmentQuery(t, execution, segment)
	execution.Close(t)

	prepareFixture(t, db, enginefixture.RatioAttributionUndefinedTotal)
	execution = openProductionExecution(t)
	harness.RunUndefinedRatioTotalQuery(t, execution, segment)
}
