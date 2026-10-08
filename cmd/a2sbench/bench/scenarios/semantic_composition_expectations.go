package scenarios

func registerSemanticCompositionCompilerExpectations() {
	compilerExpectations["cumulative_metric_with_source_filter"] = CompilerExpectation{
		Fragments:  []string{"cumulative_revenue", "OVER (", "orders.status", "WHERE"},
		Parameters: 1,
	}
	compilerExpectations["semi_additive_with_ordinary_metric"] = CompilerExpectation{
		Fragments: []string{"inventory_balance", "inventory_quantity", "warehouse"},
	}
	compilerExpectations["shared_grain_derived_with_source_metric"] = CompilerExpectation{
		Fragments: []string{"revenue", "contribution_margin", "discounts", "status"},
	}
	compilerExpectations["metric_filter_multi_metric"] = CompilerExpectation{
		Fragments:  []string{"revenue", "orders_count", "contribution_margin", "WHERE"},
		Parameters: 1,
	}
	compilerExpectations["semantic_extension_derived_metric"] = CompilerExpectation{
		Fragments: []string{"scaled_revenue", "scaled_net_revenue", "discounts", "* 2"},
	}
	compilerExpectations["custom_calendar_cumulative_with_filter"] = CompilerExpectation{
		Fragments:  []string{"rolling_3_fiscal_week_revenue", "calendar_day", "WHERE", "ROWS BETWEEN 2 PRECEDING AND CURRENT ROW"},
		Parameters: 1,
	}
}
