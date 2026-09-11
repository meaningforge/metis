package optimizer_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// The differential corpus proves that optimization preserves results. It cannot
// prove that optimization happened: 22 of its 28 scenarios compile to the same
// plan with and without the optimizer, so their differential execution runs one
// statement against itself and passes. That is not a defect -- a no-op scenario
// is the regression guard for a rule that starts firing where nothing did -- but
// it was invisible, and a scenario that silently stopped being rewritten would
// have kept passing while proving nothing.
//
// This makes each scenario say which it is, and checks the claim against the
// SemanticPlan fingerprint.
func TestOptimizerActivityMatchesEachScenarioDeclaration(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, differential := range scenarios.OptimizerDifferentialCases() {
				t.Run(differential.Scenario.Name, func(t *testing.T) {
					rewritten := rewritesPlan(t, differential.Scenario, target.Dialect)
					switch differential.Expectation {
					case scenarios.OptimizerRewritesPlan:
						if !rewritten {
							t.Fatalf("scenario declares %s but the optimizer left the plan fingerprint unchanged; "+
								"it no longer covers the transform it was added for", differential.Expectation)
						}
					case scenarios.OptimizerNoOp:
						if rewritten {
							t.Fatalf("scenario declares %s but the optimizer rewrote the plan; "+
								"a newly firing rule needs its result equivalence declared, not inherited", differential.Expectation)
						}
					default:
						t.Fatalf("scenario declares unknown expectation %q", differential.Expectation)
					}
				})
			}
		})
	}
}

// The distribution is asserted from the declarations rather than kept as a
// second list, so adding a scenario forces both an expectation and a visible
// change to this number. A corpus that drifted to all no-op would otherwise
// still be a green optimizer differential suite.
func TestOptimizerDifferentialCorpusDistribution(t *testing.T) {
	byExpectation := map[scenarios.OptimizerExpectation][]string{}
	for _, differential := range scenarios.OptimizerDifferentialCases() {
		byExpectation[differential.Expectation] = append(byExpectation[differential.Expectation], differential.Scenario.Name)
	}
	rewrites := len(byExpectation[scenarios.OptimizerRewritesPlan])
	noop := len(byExpectation[scenarios.OptimizerNoOp])
	t.Logf("optimizer differential corpus: rewrites-plan=%d no-op=%d total=%d", rewrites, noop, rewrites+noop)

	if rewrites != 6 || noop != 22 {
		t.Fatalf("corpus distribution = rewrites-plan %d, no-op %d; want 6 and 22.\n"+
			"Update this only alongside a deliberate change to the corpus -- a drop in "+
			"rewrites-plan means the suite proves less than it did.", rewrites, noop)
	}
	if total := len(scenarios.OptimizerDifferentialCore()); total != rewrites+noop {
		t.Fatalf("OptimizerDifferentialCore returns %d scenarios, declarations cover %d", total, rewrites+noop)
	}
}

// An auxiliary observation, deliberately not a classification input.
//
// A plan rewrite the renderer folds back to identical SQL is the case that
// makes plan fingerprints the right level for the classification above: the
// execution differential is structurally blind to it. Recording which scenarios
// are in that state makes a change in renderer folding visible without turning
// SQL shape into evidence about the optimizer. It asserts nothing, because
// either answer is legitimate.
func TestObservePlanRewritesThatEmitIdenticalSQL(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		var folded []string
		for _, differential := range scenarios.OptimizerDifferentialCases() {
			if differential.Expectation != scenarios.OptimizerRewritesPlan {
				continue
			}
			optimized, unoptimized, compileTarget := planScenario(t, differential.Scenario, target.Dialect)
			if renderSQL(t, optimized, compileTarget) == renderSQL(t, unoptimized, compileTarget) {
				folded = append(folded, differential.Scenario.Name)
			}
		}
		sort.Strings(folded)
		if len(folded) == 0 {
			t.Logf("%s: every plan rewrite reaches the SQL", target.Dialect)
			continue
		}
		t.Logf("%s: plan rewritten but SQL identical, so differential execution cannot observe it: %s",
			target.Dialect, strings.Join(folded, ", "))
	}
}

func rewritesPlan(t *testing.T, scenario scenarios.Scenario, dialect string) bool {
	t.Helper()
	optimized, unoptimized, _ := planScenario(t, scenario, dialect)
	optimizedFingerprint, err := semanticplan.Fingerprint(optimized)
	if err != nil {
		t.Fatalf("fingerprint optimized plan: %v", err)
	}
	unoptimizedFingerprint, err := semanticplan.Fingerprint(unoptimized)
	if err != nil {
		t.Fatalf("fingerprint unoptimized plan: %v", err)
	}
	return optimizedFingerprint != unoptimizedFingerprint
}

func renderSQL(t *testing.T, plan *semanticplan.SemanticPlan, renderer renderer.Renderer) string {
	t.Helper()
	compiled, err := compiler.CompileWithRenderer(context.Background(), plan, renderer)
	if err != nil {
		t.Fatalf("compile plan: %v", err)
	}
	sqlQuery := compiled.SqlRenderResult
	return sqlQuery.SQL
}
