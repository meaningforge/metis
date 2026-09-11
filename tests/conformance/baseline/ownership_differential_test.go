package baseline_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestDirectPlanOwnershipAgreesWithLegacyFlatFields(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			coverage := differentialCoverage{}

			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				if conversion.LoweringStrategyForPlan(plan) != conversion.SemanticLoweringCompact {
					continue
				}

				for _, node := range plan.Nodes {
					coverage.stages++
					for _, difference := range compareNodeOwnership(node, plan, &coverage) {
						t.Errorf("%s node %q: %s", scenario.Name, node.NodeBase().ID, difference)
					}
				}
			}

			coverage.assert(t)
		})
	}
}

func compareNodeOwnership(node semanticplan.SemanticPlanNode, plan *semanticplan.SemanticPlan, coverage *differentialCoverage) []string {
	var differences []string
	base := node.NodeBase()

	var source semanticplan.SemanticSourceState
	switch typed := node.(type) {
	case semanticplan.SourceAggregateNode:
		source = typed.Source
	case semanticplan.PostAggregateNode:
		source = typed.Source
	case semanticplan.JoinAggregatesNode:
		source = typed.Source
	case semanticplan.CrossJoinAggregatesNode:
		source = typed.Source
	case semanticplan.CumulativeWindowNode:
		source = typed.Source
	case semanticplan.TimeOffsetNode:
		source = typed.Source
	case semanticplan.OffsetToGrainNode:
		source = typed.Source
	case semanticplan.ConversionNode:
		source = typed.Source
	case semanticplan.SemiAdditiveNode:
		source = typed.Source
	case semanticplan.SourceSelectionNode:
		source = typed.Source
	}

	if source.Root.Name != plan.Root.Name || source.Root.Source != plan.Root.Source {
		differences = append(differences, fmt.Sprintf("node root %s/%s, flat plan root %s/%s",
			source.Root.Name, source.Root.Source, plan.Root.Name, plan.Root.Source))
	}

	nodeJoins := joinIdentities(source.Joins)
	flatJoins := joinIdentities(plan.Joins)
	if len(nodeJoins) != 0 {
		coverage.joins++
	}
	if difference := compareIdentities("joins", nodeJoins, flatJoins); difference != "" {
		differences = append(differences, difference)
	}

	nodeGrain := groupIdentities(base.OutputGrain)
	flatGrain := groupIdentities(plan.Groups)
	if len(nodeGrain) != 0 {
		coverage.grain++
	}
	if difference := compareIdentities("output grain", nodeGrain, flatGrain); difference != "" {
		differences = append(differences, difference)
	}

	nodePredicates := nodePredicateIdentities(base.Predicates)
	flatPredicates := predicateIdentities(plan.Predicates)
	if len(nodePredicates) != 0 {
		coverage.predicates++
	}
	if difference := compareIdentities("predicates", nodePredicates, flatPredicates); difference != "" {
		differences = append(differences, difference)
	}

	return differences
}

type differentialCoverage struct {
	stages     int
	joins      int
	grain      int
	predicates int
}

func (c differentialCoverage) assert(t *testing.T) {
	t.Helper()
	for _, recorded := range []struct {
		what string
		have int
		want int
	}{
		{"nodes", c.stages, 48},
		{"nodes carrying joins", c.joins, 22},
		{"nodes carrying an output grain", c.grain, 37},
		{"nodes carrying predicates", c.predicates, 16},
	} {
		if recorded.have != recorded.want {
			t.Errorf("%s = %d, recorded %d", recorded.what, recorded.have, recorded.want)
		}
	}
}

func compareIdentities(what string, node, flat []string) string {
	if strings.Join(node, ",") == strings.Join(flat, ",") {
		return ""
	}
	return fmt.Sprintf("%s differ: node [%s], flat plan [%s]", what, strings.Join(node, ", "), strings.Join(flat, ", "))
}

func joinIdentities(joins []semanticplan.Join) []string {
	out := make([]string, 0, len(joins))
	for _, join := range joins {
		out = append(out, fmt.Sprintf("%s/%s->%s/%s", join.FromDataset, join.FromSource, join.ToDataset, join.ToSource))
	}
	sort.Strings(out)
	return out
}

func groupIdentities(groups []semanticplan.GroupBy) []string {
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.Name)
	}
	sort.Strings(out)
	return out
}

func nodePredicateIdentities(predicates []semanticplan.SemanticPlanNodePredicate) []string {
	var owned []semanticplan.Predicate
	for _, predicate := range predicates {
		if predicate.Predicate != nil {
			owned = append(owned, *predicate.Predicate)
		}
	}
	return predicateIdentities(owned)
}

func predicateIdentities(predicates []semanticplan.Predicate) []string {
	out := make([]string, 0, len(predicates))
	for _, predicate := range predicates {
		out = append(out, fmt.Sprintf("%s %s %v", predicate.Filter.Field, predicate.Filter.Operator, predicate.Filter.Value))
	}
	sort.Strings(out)
	return out
}
