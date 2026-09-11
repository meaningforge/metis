package scenarios

// compilerExpectations contains only cross-dialect query-shape obligations.
// Dialect-specific expectations remain in renderer contract tests.
var compilerExpectations = map[string]CompilerExpectation{
	"aggregation_null_zero_negative":         {Fragments: []string{"AVG(orders.amount)", "MIN(orders.amount)", "MAX(orders.amount)"}},
	"grouping_null_dimension":                {Fragments: []string{"orders.status", "SUM(orders.amount)", "COUNT(DISTINCT orders.order_id)", "GROUP BY"}},
	"relationship_unmatched_facts":           {Fragments: []string{"customer.region", "JOIN", "SUM(orders.amount)", "COUNT(DISTINCT orders.order_id)"}},
	"duplicate_invariant_filtered_fanout":    {Fragments: []string{"COUNT(DISTINCT customer.customer_id)", "JOIN", "orders.status", "WHERE"}, Parameters: 1},
	"multi_hop_repeated_dimension_values":    {Fragments: []string{"geography.country", "JOIN", "SUM(orders.amount)"}},
	"joined_ratio_repeated_entities":         {Fragments: []string{"customer.customer_id", "customer.region", "SUM(orders.amount)"}},
	"metric_filter_zero_and_negative_groups": {Fragments: []string{"orders.status", "revenue", "WHERE"}, Parameters: 1},
	"ordered_ties_secondary_key":             {Fragments: []string{"orders.status", "WHERE", "ORDER BY"}, Parameters: 1},
	"time_filter_inclusive_boundaries":       {Fragments: []string{"order_date", "WHERE", "GROUP BY"}, Parameters: 2},
	"cumulative_zero_negative_periods":       {Fragments: []string{"cumulative_revenue", "OVER (", "UNBOUNDED PRECEDING"}},
	"time_offset_zero_negative_periods":      {Fragments: []string{"previous_month_revenue", "revenue", "order_date"}},
	"derived_null_negative_inputs":           {Fragments: []string{"revenue", "discounts", "contribution_margin"}},
	// One parameter, not two: both metrics read the same filtered rows, so
	// source-scan fusion emits one scan with one bound filter rather than two
	// identical scans cross-joined. The result is unchanged -- the real-engine
	// corpus executes this scenario and still gets the declared null.
	"ratio_empty_population_null": {Fragments: []string{"average_order_value", "orders_count", "WHERE"}, Parameters: 1},
	"simple_metric": {
		Fragments: []string{"SUM(orders.amount)", "revenue"},
	},
	"multiple_metrics_same_source": {
		Fragments: []string{"SUM(orders.amount)", "COUNT(DISTINCT orders.order_id)"},
	},
	"aggregation_variants": {
		Fragments: []string{"AVG(orders.amount)", "MIN(orders.amount)", "MAX(orders.amount)"},
	},
	"local_dimension": {
		Fragments: []string{"orders.status", "GROUP BY"},
	},
	"one_hop_join": {
		Fragments: []string{"JOIN", "customer.region"},
	},
	"multi_hop_join": {
		Fragments: []string{"JOIN", "geography.country"},
	},
	"multiple_joined_dimensions": {
		Fragments: []string{"customer", "product"},
	},
	"filters_order_limit": {
		Fragments:  []string{"WHERE", "ORDER BY"},
		Parameters: 3,
	},
	"cross_dataset_leaf_metric": {
		Fragments: []string{"customer.customer_id", "SUM(orders.amount)"},
	},
	"derived_metric": {
		Fragments: []string{"revenue", "discounts"},
	},
	"ratio_metric": {
		Fragments: []string{"revenue", "orders_count"},
	},
	"nested_derived_metric": {
		Fragments: []string{"contribution_margin", "revenue"},
	},
	"multi_source_derived_at_grain": {
		Fragments: []string{"revenue", "cost"},
	},
	"independent_multi_source_at_grain": {
		Fragments: []string{"revenue", "cost"},
	},
	"independent_multi_source_ungrouped": {
		Fragments: []string{"revenue", "cost"},
	},
	"time_month": {
		Fragments: []string{"order_date", "GROUP BY"},
	},
	"dimensions_only": {
		Fragments: []string{"orders.status", "customer.region", "JOIN", "GROUP BY"},
	},
	"multiple_local_dimensions": {
		Fragments: []string{"orders.status", "orders.order_date", "GROUP BY"},
	},
	"metric_local_and_joined_dimensions": {
		Fragments: []string{"orders.status", "customer.region", "JOIN", "GROUP BY"},
	},
	"multiple_filters_same_dimension": {
		Fragments:  []string{"WHERE", "orders.status", "NOT IN"},
		Parameters: 3,
	},
	"joined_dimension_filter": {
		Fragments:  []string{"JOIN", "customer.region", "WHERE"},
		Parameters: 1,
	},
	"time_filter_with_month_grain": {
		Fragments:  []string{"order_date", "WHERE", "GROUP BY"},
		Parameters: 2,
	},
	"order_by_metric_ungrouped": {
		Fragments: []string{"SUM(orders.amount)", "ORDER BY"},
	},
	"limit_without_order_by": {
		Fragments: []string{"orders.status", "GROUP BY"},
	},
	"multiple_metrics_joined_dimension": {
		Fragments: []string{"SUM(orders.amount)", "COUNT(DISTINCT orders.order_id)", "customer.region", "JOIN", "GROUP BY"},
	},
	"multiple_metrics_with_filters": {
		Fragments:  []string{"SUM(orders.amount)", "COUNT(DISTINCT orders.order_id)", "WHERE"},
		Parameters: 2,
	},
	"multiple_metrics_time_grain": {
		Fragments: []string{"SUM(orders.amount)", "COUNT(DISTINCT orders.order_id)", "GROUP BY"},
	},
	"joined_dimension_filter_order_limit": {
		Fragments:  []string{"customer.region", "JOIN", "WHERE", "ORDER BY"},
		Parameters: 1,
	},
	"multi_hop_intermediate_filter": {
		Fragments:  []string{"customer.segment", "geography.country", "JOIN", "WHERE"},
		Parameters: 1,
	},
	"multi_hop_terminal_filter": {
		Fragments:  []string{"geography.country", "JOIN", "WHERE"},
		Parameters: 1,
	},
	"dimensions_only_filter_order_limit": {
		Fragments:  []string{"orders.status", "customer.region", "JOIN", "WHERE", "ORDER BY", "GROUP BY"},
		Parameters: 1,
	},
	"cumulative_metric_by_month": {
		Fragments: []string{"cumulative_revenue", "OVER (", "ORDER BY", "UNBOUNDED PRECEDING", "CURRENT ROW"},
	},
	"cumulative_metric_by_month_and_region": {
		Fragments: []string{"cumulative_revenue", "OVER (", "PARTITION BY", "region", "ORDER BY", "UNBOUNDED PRECEDING"},
	},
	"cumulative_metric_by_quarter": {
		Fragments: []string{"cumulative_revenue", "OVER (", "ORDER BY", "UNBOUNDED PRECEDING", "CURRENT ROW"},
	},
	"cumulative_metric_by_quarter_and_region": {
		Fragments: []string{"cumulative_revenue", "OVER (", "PARTITION BY", "region", "ORDER BY", "UNBOUNDED PRECEDING"},
	},
	"time_offset_previous_month": {
		Fragments: []string{"previous_month_revenue", "revenue", "order_date"},
	},
	"time_offset_current_and_previous_by_region": {
		Fragments: []string{"previous_month_revenue", "revenue", "region", "FULL OUTER JOIN"},
	},
	"time_offset_previous_quarter": {
		Fragments: []string{"previous_quarter_revenue", "revenue", "order_date"},
	},
	"time_offset_previous_year": {
		Fragments: []string{"previous_year_revenue", "revenue", "order_date"},
	},
	"time_offset_period_over_period_growth": {
		Fragments: []string{"revenue_growth_rate", "revenue", "previous_month_revenue"},
	},
	"metric_filter_source_metric": {
		Fragments:  []string{"revenue", "WHERE"},
		Parameters: 1,
	},
	"metric_filter_hidden_metric": {
		Fragments:  []string{"orders_count", "revenue", "WHERE"},
		Parameters: 1,
	},
	"metric_filter_derived_metric": {
		Fragments:  []string{"contribution_margin", "revenue", "WHERE", "BETWEEN"},
		Parameters: 2,
	},
	"metric_filter_time_offset_metric": {
		Fragments:  []string{"previous_month_revenue", "revenue", "WHERE"},
		Parameters: 1,
	},
	"metric_filter_with_dimension_filter": {
		Fragments:  []string{"orders.status", "revenue", "WHERE"},
		Parameters: 2,
	},
	"metric_filter_cumulative_metric": {
		Fragments:  []string{"cumulative_revenue", "OVER (", "UNBOUNDED PRECEDING", "WHERE"},
		Parameters: 1,
	},
	"semi_additive_latest_snapshot": {
		Fragments: []string{"inventory_balance", "inventory_quantity", "snapshot_date", "warehouse"},
	},
	"semi_additive_as_of_time": {
		Fragments:  []string{"inventory_balance", "inventory_quantity", "snapshot_date", "warehouse", "WHERE"},
		Parameters: 1,
	},
	"semi_additive_as_of_with_dimension_filter": {
		Fragments:  []string{"inventory_balance", "inventory_quantity", "snapshot_date", "warehouse", "WHERE"},
		Parameters: 2,
	},
	"semi_additive_since_time": {
		Fragments:  []string{"inventory_balance", "inventory_quantity", "snapshot_date", "warehouse", "WHERE"},
		Parameters: 1,
	},
	"semi_additive_between_time": {
		Fragments:  []string{"inventory_balance", "inventory_quantity", "snapshot_date", "warehouse", "WHERE", "BETWEEN"},
		Parameters: 2,
	},
	"distinct_dimension_values": {
		Fragments:  []string{"region", "country", "GROUP BY", "WHERE", "ORDER BY"},
		Parameters: 1,
	},
	"metric_definition_filter_pre_aggregation": {
		Fragments:  []string{"gold_revenue", "tier", "JOIN", "WHERE"},
		Parameters: 1,
	},
	"metric_definition_filter_post_aggregation": {
		Fragments:  []string{"average_order_value", "revenue", "order_count", "WHERE"},
		Parameters: 1,
	},
	"semi_additive_first_with_tie_break": {
		Fragments: []string{"inventory_first_tie_break", "snapshot_date", "snapshot_sequence", "warehouse"},
	},
	"semi_additive_last_with_tie_break": {
		Fragments: []string{"inventory_last_tie_break", "snapshot_date", "snapshot_sequence", "warehouse"},
	},
	"semi_additive_first_skip_null": {
		Fragments: []string{"inventory_first_skip_null", "snapshot_sequence", "IS NOT NULL"},
	},
	"semi_additive_last_skip_null": {
		Fragments: []string{"inventory_last_skip_null", "snapshot_sequence", "IS NOT NULL"},
	},
	"semi_additive_queried_week": {
		Fragments: []string{"inventory_balance", "snapshot_date"},
	},
	"semi_additive_window_group_sum": {
		Fragments: []string{"inventory_window_sum", "warehouse", "SUM("},
	},
	"semi_additive_window_group_min": {
		Fragments: []string{"inventory_window_min", "warehouse", "MIN("},
	},
	"semi_additive_window_group_max": {
		Fragments: []string{"inventory_window_max", "warehouse", "MAX("},
	},
	"conversion_rate_by_campaign": {
		Fragments:  []string{"signup_to_purchase_rate", "campaign", "__metis_conversion_base_population", "__metis_conversion_assigned", "NULLIF"},
		Parameters: 1,
	},
	"conversion_count_by_campaign": {
		Fragments:  []string{"signup_conversions", "campaign", "__metis_conversion_assigned", "__metis_assigned_value"},
		Parameters: 1,
	},
	"conversion_rate_filtered_campaign": {
		Fragments:  []string{"signup_to_purchase_rate", "campaign", "__metis_conversion_base_population", "__metis_conversion_candidates", "NULLIF"},
		Parameters: 3,
	},
	"conversion_count_filtered_campaign": {
		Fragments:  []string{"signup_conversions", "campaign", "__metis_conversion_assigned", "__metis_assigned_value"},
		Parameters: 3,
	},
	"offset_to_grain_year_start_by_month_region": {
		Fragments: []string{"revenue_at_year_start", "FIRST_VALUE", "PARTITION BY", "ORDER BY", "region"},
	},
	"offset_to_grain_missing_boundary_stays_zero": {
		Fragments: []string{"revenue_at_year_start", "__calendar", "__sparse", "COALESCE", "FIRST_VALUE"},
	},
	"custom_offset_to_grain_missing_boundary_stays_zero": {
		Fragments: []string{"revenue_at_fiscal_year_start", "__metis_offset_to_grain_periods", "fiscal_week_start", "fiscal_year_start", "__metis_boundary_bucket", "__metis_dense_ordinal"},
	},
	"custom_calendar_previous_fiscal_week": {
		Fragments: []string{"previous_fiscal_week_revenue", "__metis_calendar_periods", "fiscal_week_start", "fiscal_week_index"},
	},
	"custom_calendar_dense_missing_period": {
		Fragments: []string{"previous_fiscal_week_revenue", "__periods", "__sparse", "__metis_dense_ordinal", "COALESCE"},
	},
	"custom_calendar_rolling_three_fiscal_weeks": {
		Fragments: []string{"rolling_3_fiscal_week_revenue", "__periods", "__metis_dense_ordinal", "ROWS BETWEEN 2 PRECEDING AND CURRENT ROW"},
	},
	"custom_calendar_fiscal_quarter_to_date": {
		Fragments: []string{"fiscal_quarter_to_date_revenue", "__metis_custom_gtd_periods", "fiscal_quarter_start", "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW"},
	},
	"custom_calendar_semi_additive_last_snapshot": {
		Fragments: []string{"inventory_balance", "fiscal_week_start", "__metis_ordered_snapshot_date", "snapshot_date"},
	},
}
