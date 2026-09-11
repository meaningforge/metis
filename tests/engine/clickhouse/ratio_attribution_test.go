package clickhouse_test

import (
	"testing"

	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestRatioAttributionExecutionEvidence(t *testing.T) {
	url := clickHouseURL(t)
	prepareFixture(t, url, enginefixture.RatioAttribution)
	execution := openProductionExecution(t)
	queries := harness.CompileRatioAttributionBundleForExecution(t, execution, []string{"segment", "channel"}, true)
	if len(queries) != 2 || queries[0].Dimension != "channel" || queries[1].Dimension != "segment" {
		t.Fatalf("ratio attribution bundle order = %#v", queries)
	}
	harness.RunDefinedRatioAttributionBundleEvidence(t, execution, queries)
	segment := queries[1].CompiledQuery
	execution.Close(t)

	prepareFixture(t, url, enginefixture.RatioAttributionUndefinedSegment)
	execution = openProductionExecution(t)
	harness.RunUndefinedRatioSegmentQuery(t, execution, segment)
	execution.Close(t)

	prepareFixture(t, url, enginefixture.RatioAttributionUndefinedTotal)
	execution = openProductionExecution(t)
	harness.RunUndefinedRatioTotalQuery(t, execution, segment)
}
