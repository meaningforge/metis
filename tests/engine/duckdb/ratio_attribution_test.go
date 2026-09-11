//go:build duckdb

package duckdb_test

import (
	"context"
	"strings"
	"testing"

	duckdbfixture "github.com/meaningforge/metis/tests/engine/duckdb/fixture"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestRatioAttributionExecutionEvidence(t *testing.T) {
	backend, err := duckdbfixture.New(databasePath(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = backend.Close(ctx) })
	if err := backend.PrepareFixture(ctx, enginefixture.RatioAttribution); err != nil {
		t.Fatal(err)
	}
	execution := openProductionExecution(t, backend.Database)
	queries := harness.CompileRatioAttributionBundleForExecution(t, execution, []string{"segment", "channel"}, true)
	if len(queries) != 2 || queries[0].Dimension != "channel" || queries[1].Dimension != "segment" {
		t.Fatalf("ratio attribution bundle order = %#v", queries)
	}
	for _, compiled := range queries {
		if scans := strings.Count(compiled.SQLRenderResult.SQL, `FROM "analytics"."ratio_attribution_events"`); scans != 2 {
			t.Fatalf("ratio attribution dimension %q source scans = %d, want one per period\nSQL:\n%s", compiled.Dimension, scans, compiled.SQLRenderResult.SQL)
		}
	}
	harness.RunDefinedRatioAttributionBundleEvidence(t, execution, queries)
	segment := queries[1].CompiledQuery
	execution.Close(t)

	if err := backend.PrepareFixture(ctx, enginefixture.RatioAttributionUndefinedSegment); err != nil {
		t.Fatal(err)
	}
	execution = openProductionExecution(t, backend.Database)
	harness.RunUndefinedRatioSegmentQuery(t, execution, segment)
	execution.Close(t)

	if err := backend.PrepareFixture(ctx, enginefixture.RatioAttributionUndefinedTotal); err != nil {
		t.Fatal(err)
	}
	execution = openProductionExecution(t, backend.Database)
	harness.RunUndefinedRatioTotalQuery(t, execution, segment)
}
