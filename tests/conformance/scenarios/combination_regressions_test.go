package scenarios

import "testing"

// Keep six risk groups tied to canonical result evidence, not just SQL text.
// Unsupported compositions are asserted by compiler rejection tests instead.
func TestCombinationRiskGroupsHaveIndependentResultOracles(t *testing.T) {
	groups := map[string][]string{
		"calendar":              {"calendar_month_end_and_missing_period", "custom_calendar_dense_missing_period", "offset_to_grain_missing_boundary_stays_zero"},
		"rolling input range":   {"rolling_window_before_output_range", "custom_calendar_rolling_three_fiscal_weeks", "custom_calendar_rolling_filter_preserves_lookback"},
		"final order and limit": {"nested_derived_order_limit_after_aggregation", "aggregate_order_limit_after_grouping"},
		"distinct regroup":      {"distinct_entity_across_periods_global", "distinct_entity_across_periods_grouped"},
		"fanout":                {"fanout_base_population_unchanged", "duplicate_invariant_filtered_fanout"},
		"semi-additive":         {"semi_additive_last_ties_then_account_rollup", "semi_additive_last_with_tie_break", "semi_additive_queried_week"},
	}
	for group, names := range groups {
		for _, name := range names {
			s, ok := ByName(name)
			if !ok || s.Verification.Result != ContractRequired || s.ExpectedResult == nil || len(s.ExpectedResult.Rows) == 0 {
				t.Fatalf("%s / %s lacks executable result oracle", group, name)
			}
		}
	}
	for _, name := range groups["final order and limit"] {
		s, _ := ByName(name)
		if s.Query.Limit == nil || *s.Query.Limit != 1 || len(s.Query.OrderBy) != 1 {
			t.Fatalf("%s no longer discriminates final top-one selection", name)
		}
	}
}
