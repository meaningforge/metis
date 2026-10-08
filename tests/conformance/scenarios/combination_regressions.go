package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

// The expected values are manual oracles, not captured compiler/engine output.
var combinationRegressions = func() []Scenario {
	compilerExpectations["calendar_month_end_and_missing_period"] = CompilerExpectation{Fragments: []string{"revenue", "previous_month_revenue"}}
	compilerExpectations["rolling_window_before_output_range"] = CompilerExpectation{Fragments: []string{"cumulative_revenue", "ROWS BETWEEN 2 PRECEDING AND CURRENT ROW", "WHERE"}, Parameters: 4}
	compilerExpectations["nested_derived_order_limit_after_aggregation"] = CompilerExpectation{Fragments: []string{"nested_margin_ratio", "ORDER BY", "DESC"}}
	compilerExpectations["aggregate_order_limit_after_grouping"] = CompilerExpectation{Fragments: []string{"revenue", "ORDER BY", "DESC"}}
	compilerExpectations["fanout_base_population_unchanged"] = CompilerExpectation{Fragments: []string{"SUM(orders.amount)"}}
	compilerExpectations["semi_additive_last_ties_then_account_rollup"] = CompilerExpectation{Fragments: []string{"inventory_window_sum", "snapshot_sequence"}}
	compilerExpectations["distinct_entity_across_periods_global"] = CompilerExpectation{Fragments: []string{"COUNT(DISTINCT orders.customer_id)", "WHERE"}, Parameters: 1}
	compilerExpectations["distinct_entity_across_periods_grouped"] = CompilerExpectation{Fragments: []string{"COUNT(DISTINCT orders.customer_id)", "WHERE"}, Parameters: 1}
	return []Scenario{
		// Select last per account with sequence tie-break, THEN sum:
		// w1=40, w2=25 =>65. Summing all historical rows would yield145.
		resultFixtureScenario(fixtures.SemiAdditiveTieRollup, "semi_additive_last_ties_then_account_rollup", CategoryComposition,
			[]Capability{CapabilityAggregation, CapabilitySemiAdditive, CapabilitySemiAdditiveTieBreak, CapabilitySemiAdditiveWindowGrouping, CapabilitySemiAdditiveRollup},
			fixtureSemanticQuery(fixtures.SemiAdditiveTieRollup, []string{"inventory_window_sum"}, nil), ResultLiteral{"65"}),
		// One entity occurs in January and March. Global count is 1,
		// not the sum (2) of the two per-month counts.
		resultFixtureScenario(fixtures.CommercePeriodEdges, "distinct_entity_across_periods_global", CategoryComposition,
			[]Capability{CapabilityAggregation, CapabilityRelationship, CapabilityFilter}, distinctPeriodCombination(false), ResultLiteral{"1"}),
		resultFixtureScenario(fixtures.CommercePeriodEdges, "distinct_entity_across_periods_grouped", CategoryComposition,
			[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityRelationship, CapabilityFilter}, distinctPeriodCombination(true), ResultLiteral{"2026-01-01", "1"}, ResultLiteral{"2026-03-01", "1"}),
		// Merely declaring a one-to-many relationship must not join it:
		// 100+50=150, not 100*3+50*2=400. Traversal is rejected separately.
		resultFixtureScenario(fixtures.OrderDetails, "fanout_base_population_unchanged", CategoryJoin,
			[]Capability{CapabilityAggregation}, fixtureSemanticQuery(fixtures.OrderDetails, []string{"revenue"}, nil), ResultLiteral{"150"}),
		// January 31 -> February's bucket, not March 3. February has no
		// source row: NULL is distinct from dense-calendar zero-fill.
		resultFixtureScenario(fixtures.CommercePeriodEdges, "calendar_month_end_and_missing_period", CategoryTime,
			[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset}, func() query.SemanticQuery {
				month := query.TimeGrainMonth
				return fixtureSemanticQuery(fixtures.CommercePeriodEdges, []string{"revenue", "previous_month_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &month}})
			}(), ResultLiteral{"2026-01-01", "10", NullValue}, ResultLiteral{"2026-02-01", NullValue, "10"}, ResultLiteral{"2026-03-01", "30", NullValue}, ResultLiteral{"2026-04-01", NullValue, "30"}),
		// March needs January's 10, even though output begins in March.
		// Incorrect early clipping would give 30 instead of 40.
		resultFixtureScenario(fixtures.CommerceRolling, "rolling_window_before_output_range", CategoryTime,
			[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityCumulative, CapabilityTimeGrain}, func() query.SemanticQuery {
				q := customGrainQuery(fixtures.CommerceRolling, "cumulative_revenue", "order_date", query.TimeGrainMonth)
				q.Filters = []query.Filter{{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-03-01", "2026-03-31"}}}
				return q
			}(), ResultLiteral{"2026-03-01", "40"}),
		// APAC: (50+70-0)/(50+70)=1; EU: (100-90)/100=.1.
		// Sorting/limiting raw inputs first picks EU's single 100 row.
		resultFixtureScenario(fixtures.CommerceOrdering, "nested_derived_order_limit_after_aggregation", CategoryComposition,
			[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship, CapabilityDerived, CapabilityRatio, CapabilityOrdering}, orderedCombination("nested_margin_ratio"), ResultLiteral{"APAC", "1"}),
		// APAC 50+70=120 beats EU 100 despite EU having the largest row.
		resultFixtureScenario(fixtures.CommerceOrdering, "aggregate_order_limit_after_grouping", CategoryComposition,
			[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship, CapabilityOrdering}, orderedCombination("revenue"), ResultLiteral{"APAC", "120"}),
	}
}()

func orderedCombination(metric string) query.SemanticQuery {
	q := fixtureSemanticQuery(fixtures.CommerceOrdering, []string{metric}, []query.DimensionRef{{Name: "region"}})
	q.OrderBy = []query.OrderBy{{Field: metric, Direction: query.SortDesc}}
	limit := 1
	q.Limit = &limit
	return q
}

func distinctPeriodCombination(grouped bool) query.SemanticQuery {
	q := fixtureSemanticQuery(fixtures.CommercePeriodEdges, []string{"unique_order_customers"}, nil)
	q.Filters = []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}}
	if grouped {
		month := query.TimeGrainMonth
		q.Dimensions = []query.DimensionRef{{Name: "order_date", Grain: &month}}
	}
	return q
}
