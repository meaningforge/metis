package baseline_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestEveryPlanOwnsAValidatedNodeDAG(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				if len(plan.Nodes) == 0 {
					t.Errorf("%s: plan owns no semantic nodes", scenario.Name)
					continue
				}

				seen := map[string]bool{}
				for _, node := range plan.Nodes {
					base := node.NodeBase()
					if base.ID == "" {
						t.Errorf("%s: a plan node has no id", scenario.Name)
						continue
					}
					if seen[base.ID] {
						t.Errorf("%s: plan node %q appears twice", scenario.Name, base.ID)
					}
					seen[base.ID] = true
					if node.Kind() == "" {
						t.Errorf("%s: plan node %q has no kind", scenario.Name, base.ID)
					}
					if base.Boundary == "" {
						t.Errorf("%s: plan node %q has no boundary", scenario.Name, base.ID)
					}
				}

				produced := map[string]bool{}
				for _, node := range plan.Nodes {
					base := node.NodeBase()
					for _, input := range base.Inputs {
						if !produced[input.NodeID] {
							t.Errorf("%s: plan node %q depends on %q, which is not an earlier node", scenario.Name, base.ID, input.NodeID)
						}
					}
					produced[base.ID] = true
				}
			}
		})
	}
}

func TestEveryRequestedMetricIsEvaluatedByAPlanNode(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				evaluated := map[string]bool{}
				for _, node := range plan.Nodes {
					for _, metric := range semanticPlanNodeMetrics(node) {
						evaluated[metric] = true
					}
				}
				for _, metric := range scenario.Query.Metrics {
					if !evaluated[metric.Name] {
						t.Errorf("%s: metric %q is requested but no plan node evaluates it", scenario.Name, metric.Name)
					}
				}
			}
		})
	}
}

func semanticPlanNodeMetrics(node semanticplan.SemanticPlanNode) []string {
	switch node := node.(type) {
	case semanticplan.SourceAggregateNode:
		return node.MetricState.Metrics
	case semanticplan.PostAggregateNode:
		return node.MetricState.Metrics
	case semanticplan.JoinAggregatesNode:
		return node.MetricState.Metrics
	case semanticplan.CrossJoinAggregatesNode:
		return node.MetricState.Metrics
	case semanticplan.CumulativeWindowNode:
		return node.MetricState.Metrics
	case semanticplan.TimeOffsetNode:
		return node.MetricState.Metrics
	case semanticplan.OffsetToGrainNode:
		return node.MetricState.Metrics
	case semanticplan.ConversionNode:
		return node.MetricState.Metrics
	case semanticplan.SemiAdditiveNode:
		return node.MetricState.Metrics
	default:
		return nil
	}
}

func TestMetricFreeQueriesOwnATypedSourceSelectionNode(t *testing.T) {
	modes := map[string]semanticplan.SourceSelectionMode{
		"dimensions_only":                    semanticplan.SourceSelectionGroupedValues,
		"dimensions_only_filter_order_limit": semanticplan.SourceSelectionGroupedValues,
		"distinct_dimension_values":          semanticplan.SourceSelectionDistinctValues,
		"temporal_join_half_open_boundary":   semanticplan.SourceSelectionGroupedValues,
		"temporal_join_open_ended_version":   semanticplan.SourceSelectionGroupedValues,
		"temporal_join_reverse_traversal":    semanticplan.SourceSelectionGroupedValues,
	}

	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			var observed []string
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				if len(scenario.Query.Metrics) != 0 {
					for _, node := range plan.Nodes {
						if node.Kind() == semanticplan.SemanticPlanNodeSourceSelection {
							t.Errorf("%s: metric-bearing query owns a source-selection node", scenario.Name)
						}
					}
					continue
				}
				observed = append(observed, scenario.Name)

				if len(plan.Nodes) != 1 {
					t.Errorf("%s: metric-free query owns %d nodes, want one source selection", scenario.Name, len(plan.Nodes))
					continue
				}
				node, ok := plan.Nodes[0].(semanticplan.SourceSelectionNode)
				if !ok {
					t.Errorf("%s: node %q has type %T, want SourceSelectionNode", scenario.Name, plan.Nodes[0].NodeBase().ID, plan.Nodes[0])
					continue
				}
				if want, ok := modes[scenario.Name]; ok && node.Mode != want {
					t.Errorf("%s: mode = %q, want %q", scenario.Name, node.Mode, want)
				}
				if len(node.Base.Dimensions) != len(scenario.Query.Dimensions) {
					t.Errorf("%s: node selects %d dimensions, query asked for %d", scenario.Name, len(node.Base.Dimensions), len(scenario.Query.Dimensions))
				}
			}

			sort.Strings(observed)
			recorded := make([]string, 0, len(modes))
			for name := range modes {
				recorded = append(recorded, name)
			}
			sort.Strings(recorded)
			if diff := difference(observed, recorded); len(diff) != 0 {
				t.Errorf("these scenarios are metric free and not recorded:\n  %s", strings.Join(diff, "\n  "))
			}
			if diff := difference(recorded, observed); len(diff) != 0 {
				t.Errorf("these scenarios are recorded as metric free but no longer are:\n  %s", strings.Join(diff, "\n  "))
			}
		})
	}
}
