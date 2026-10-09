package baseline_test

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// SUM over an unproven one-to-many traversal is outside the current contract.
// Do not accept the plausible but wrong 400 from the 100/50, 3/2-detail fixture.
func TestOrderAmountFanoutFailsClosedAcrossTargets(t *testing.T) {
	base, ok := scenarios.ByName("fanout_base_population_unchanged")
	if !ok {
		t.Fatal("missing canonical fanout scenario")
	}
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			q := base
			q.Query.Dimensions = []query.DimensionRef{{Name: "kind"}}
			_, _, err := baseline.PlanScenario(q, target.Dialect)
			var typed *serrors.Error
			if !errors.As(err, &typed) || typed.Code != serrors.ErrUnsupportedRelationshipFanout {
				t.Fatalf("fanout plan error = %v, want %s", err, serrors.ErrUnsupportedRelationshipFanout)
			}
		})
	}
}
