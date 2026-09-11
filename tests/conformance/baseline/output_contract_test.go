package baseline_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// The output contract must agree with the equivalent plan fields across the
// conformance corpus. In particular, graph-derived grain must match resolved
// dimensions, and copying projections, ordering, and limits must preserve them.
func TestOutputContractAgreesWithTheFlatPlanFields(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			coverage := outputCoverage{}

			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}

				if difference := compareIdentities("grain", groupIdentities(plan.Output.Grain), groupIdentities(plan.Groups)); difference != "" {
					t.Errorf("%s: %s\n\nA plan that groups by one thing and reports another is the divergence "+
						"duplicate output authority exists to allow.", scenario.Name, difference)
				}
				if difference := compareIdentities("projections", projectionIdentities(plan.Output.Projections), projectionIdentities(plan.Projections)); difference != "" {
					t.Errorf("%s: %s", scenario.Name, difference)
				}
				if difference := compareIdentities("ordering", sortIdentities(plan.Output.OrderBy), sortIdentities(plan.Sorts)); difference != "" {
					t.Errorf("%s: %s", scenario.Name, difference)
				}
				if (plan.Output.Limit == nil) != (plan.Limit == nil) {
					t.Errorf("%s: limit is owned by one of contract/plan and not the other", scenario.Name)
				} else if plan.Output.Limit != nil && *plan.Output.Limit != *plan.Limit {
					t.Errorf("%s: contract limit %d, plan limit %d", scenario.Name, *plan.Output.Limit, *plan.Limit)
				}

				if len(plan.Output.Projections) != 0 {
					coverage.projections++
				}
				if len(plan.Output.Grain) != 0 {
					coverage.grain++
				}
				if len(plan.Output.OrderBy) != 0 {
					coverage.ordering++
				}
				if plan.Output.Limit != nil {
					coverage.limit++
				}
				if len(plan.Output.Predicates) != 0 {
					coverage.predicates++
				}
			}

			coverage.assert(t)
		})
	}
}

// Final predicates reach the contract unwrapped, and they are the ones the
// staged path removed from plan.Predicates. So the contract holding them is the
// only place a reader can still find them -- which is the point, and also why
// losing them would be silent without this.
func TestFinalPredicatesSurviveIntoTheOutputContract(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			var carried []string
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				if len(plan.Output.Predicates) == 0 {
					continue
				}
				carried = append(carried, scenario.Name)

				for _, predicate := range plan.Output.Predicates {
					if predicate.Filter.Field == "" {
						t.Errorf("%s: a final predicate reached the contract with no field", scenario.Name)
					}
					// A final predicate that also survived in plan.Predicates
					// would be applied twice when lowering from the
					// contract.
					for _, remaining := range plan.Predicates {
						if remaining.Filter.Field == predicate.Filter.Field && remaining.Filter.Operator == predicate.Filter.Operator {
							t.Errorf("%s: final predicate on %q is in both the contract and plan.Predicates",
								scenario.Name, predicate.Filter.Field)
						}
					}
				}
			}
			sort.Strings(carried)
			if len(carried) != 10 {
				t.Errorf("%d scenarios carry final predicates, recorded 10:\n  %s\n\n"+
					"Re-record a deliberate change; a fall means the contract is carrying less than it reads.",
					len(carried), strings.Join(carried, "\n  "))
			}
		})
	}
}

type outputCoverage struct {
	projections int
	grain       int
	ordering    int
	limit       int
	predicates  int
}

func (c outputCoverage) assert(t *testing.T) {
	t.Helper()
	for _, recorded := range []struct {
		what string
		have int
		want int
	}{
		{"plans with projections", c.projections, 97},
		{"plans with an output grain", c.grain, 80},
		{"plans with ordering", c.ordering, 6},
		{"plans with a limit", c.limit, 6},
		{"plans with final predicates", c.predicates, 10},
	} {
		if recorded.have != recorded.want {
			t.Errorf("%s = %d, recorded %d.\n\nThe contract's reach changed; re-record a deliberate move.",
				recorded.what, recorded.have, recorded.want)
		}
	}
}

func projectionIdentities(projections []semanticplan.Projection) []string {
	out := make([]string, 0, len(projections))
	for _, projection := range projections {
		out = append(out, fmt.Sprintf("%s/%s", projection.Name, projection.Kind))
	}
	sort.Strings(out)
	return out
}

func sortIdentities(sorts []semanticplan.Sort) []string {
	out := make([]string, 0, len(sorts))
	for _, sort := range sorts {
		out = append(out, fmt.Sprintf("%s/%s/%s", sort.Name, sort.Kind, sort.Direction))
	}
	return out
}
