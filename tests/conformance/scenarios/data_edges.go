package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

// dataEdgeScenarios reuse the canonical commerce semantics with a deliberately
// adversarial physical data world. They turn SQL-valid but low-discrimination
// examples into result oracles for NULL, zero, negative, join, tie, and time
// boundary behavior without introducing engine-specific semantic assets.
var dataEdgeScenarios = []Scenario{
	resultFixtureScenario(fixtures.CommerceAdversarial, "aggregation_null_zero_negative", CategoryMetric, []Capability{CapabilityAggregation},
		fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"average_order_amount", "minimum_order_amount", "maximum_order_amount"}, nil),
		ResultLiteral{"20", "-50", "100"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "grouping_null_dimension", CategoryDimension, []Capability{CapabilityAggregation, CapabilityDimension},
		fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue", "orders_count"}, []query.DimensionRef{{Name: "status"}}),
		ResultLiteral{NullValue, "10", "1"}, ResultLiteral{"paid", "125", "4"}, ResultLiteral{"pending", "25", "1"},
		ResultLiteral{"refunded", "0", "1"}, ResultLiteral{"cancelled", "0", "1"}, ResultLiteral{"failed", "0", "1"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "relationship_unmatched_facts", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship},
		fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue", "orders_count"}, []query.DimensionRef{{Name: "region"}}),
		ResultLiteral{"APAC", "185", "5"}, ResultLiteral{"EU", "-50", "3"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "duplicate_invariant_filtered_fanout", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship, CapabilityFilter}, func() query.SemanticQuery {
		q := fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"customers_with_matching_orders"}, nil)
		q.Filters = []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}}
		return q
	}(), ResultLiteral{"3"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "multi_hop_repeated_dimension_values", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship},
		fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue"}, []query.DimensionRef{{Name: "country"}}),
		ResultLiteral{"JP", "185"}, ResultLiteral{"DE", "-50"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "joined_ratio_repeated_entities", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship, CapabilityRatio},
		fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue_per_customer"}, []query.DimensionRef{{Name: "region"}}),
		ResultLiteral{"APAC", "92.5"}, ResultLiteral{"EU", "-50"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "metric_filter_zero_and_negative_groups", CategoryFilter, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityMetricFilter}, func() query.SemanticQuery {
		q := fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "revenue", Operator: query.FilterLTE, Value: 0}}
		return q
	}(), ResultLiteral{"refunded", "0"}, ResultLiteral{"cancelled", "0"}, ResultLiteral{"failed", "0"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "ordered_ties_secondary_key", CategoryQueryIntent, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityOrdering}, func() query.SemanticQuery {
		q := fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "status", Operator: query.FilterNotIn, Value: []string{"pending"}}}
		q.OrderBy = []query.OrderBy{{Field: "revenue", Direction: query.SortDesc}, {Field: "status", Direction: query.SortAsc}}
		limit := 3
		q.Limit = &limit
		return q
	}(), ResultLiteral{"paid", "125"}, ResultLiteral{"cancelled", "0"}, ResultLiteral{"failed", "0"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "time_filter_inclusive_boundaries", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityTimeGrain}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		q := fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
		q.Filters = []query.Filter{{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-01-01", "2026-02-01"}}}
		return q
	}(), ResultLiteral{"2026-01-01", "0"}, ResultLiteral{"2026-02-01", "-25"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "cumulative_zero_negative_periods", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityCumulative}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2025-12-01", "100"}, ResultLiteral{"2026-01-01", "100"}, ResultLiteral{"2026-02-01", "75"}, ResultLiteral{"2026-03-01", "160"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "time_offset_zero_negative_periods", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"previous_month_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", "100"}, ResultLiteral{"2026-02-01", "0"}, ResultLiteral{"2026-03-01", "-25"}, ResultLiteral{"2026-04-01", "85"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "derived_null_negative_inputs", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDerived},
		fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"revenue", "discounts", "contribution_margin"}, nil),
		ResultLiteral{"160", "12", "148"}),
	resultFixtureScenario(fixtures.CommerceAdversarial, "ratio_empty_population_null", CategoryComposition, []Capability{CapabilityAggregation, CapabilityFilter, CapabilityRatio}, func() query.SemanticQuery {
		q := fixtureSemanticQuery(fixtures.CommerceAdversarial, []string{"average_order_value"}, nil)
		q.Filters = []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "does_not_exist"}}
		return q
	}(), ResultLiteral{NullValue}),
}
