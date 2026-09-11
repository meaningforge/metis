package baseline_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Record the exact set of queries that lower to a single SELECT. A query
// changing shape should be visible by name rather than only by fingerprint.
func TestCompactCorpusIsRecorded(t *testing.T) {
	recorded := []string{
		"aggregation_null_zero_negative",
		"aggregation_variants",
		"cross_dataset_leaf_metric",
		"dimensions_only",
		"dimensions_only_filter_order_limit",
		"distinct_dimension_values",
		"duplicate_invariant_filtered_fanout",
		"filters_order_limit",
		"grouping_null_dimension",
		"joined_dimension_filter",
		"joined_dimension_filter_order_limit",
		"joined_ratio_repeated_entities",
		"limit_without_order_by",
		"local_dimension",
		"metric_local_and_joined_dimensions",
		"multi_hop_intermediate_filter",
		"multi_hop_join",
		"multi_hop_repeated_dimension_values",
		"multi_hop_terminal_filter",
		"multiple_filters_same_dimension",
		"multiple_joined_dimensions",
		"multiple_local_dimensions",
		"multiple_metrics_joined_dimension",
		"multiple_metrics_same_source",
		"multiple_metrics_time_grain",
		"multiple_metrics_with_filters",
		"one_hop_join",
		"order_by_metric_ungrouped",
		"ordered_ties_secondary_key",
		"relationship_unmatched_facts",
		"simple_metric",
		"temporal_join_half_open_boundary",
		"temporal_join_open_ended_version",
		"temporal_join_point_in_time",
		"temporal_join_reverse_traversal",
		"time_filter_inclusive_boundaries",
		"time_filter_with_month_grain",
		"time_month",
	}

	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			var compact []string
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				if conversion.LoweringStrategyForPlan(plan) == conversion.SemanticLoweringCompact {
					compact = append(compact, scenario.Name)
				}
			}
			sort.Strings(compact)
			if diff := difference(compact, recorded); len(diff) != 0 {
				t.Errorf("these lower compact and are not recorded:\n  %s", strings.Join(diff, "\n  "))
			}
			if diff := difference(recorded, compact); len(diff) != 0 {
				t.Errorf("these are recorded as compact but no longer lower that way:\n  %s", strings.Join(diff, "\n  "))
			}
		})
	}
}
