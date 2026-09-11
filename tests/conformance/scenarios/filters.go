package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

var filterScenarios = []Scenario{
	resultScenario("filters_order_limit", CategoryFilter, []Capability{CapabilityAggregation, CapabilityFilter, CapabilityOrdering}, func() query.SemanticQuery {
		limit := 25
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}, {Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-01-01", "2026-12-31"}}}
		q.OrderBy = []query.OrderBy{{Field: "revenue", Direction: query.SortDesc}, {Field: "status", Direction: query.SortAsc}}
		q.Limit = &limit
		return q
	}(), ResultLiteral{"paid", "300"}),
	resultScenario("multiple_filters_same_dimension", CategoryFilter, []Capability{CapabilityAggregation, CapabilityFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{
			{Field: "status", Operator: query.FilterNEQ, Value: "cancelled"},
			{Field: "status", Operator: query.FilterNotIn, Value: []string{"pending", "fraud"}},
		}
		return q
	}(), ResultLiteral{"paid", "300"}, ResultLiteral{"refunded", "50"}),
	resultScenario("joined_dimension_filter", CategoryFilter, []Capability{CapabilityAggregation, CapabilityFilter, CapabilityRelationship}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
		return q
	}(), ResultLiteral{"paid", "300"}),
	resultScenario("multiple_metrics_with_filters", CategoryFilter, []Capability{CapabilityAggregation, CapabilityFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue", "orders_count"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{
			{Field: "status", Operator: query.FilterEQ, Value: "paid"},
			{Field: "order_date", Operator: query.FilterGTE, Value: "2026-01-01"},
		}
		return q
	}(), ResultLiteral{"paid", "300", "2"}),
	resultScenario("metric_filter_source_metric", CategoryFilter, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityMetricFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "revenue", Operator: query.FilterGT, Value: 100}}
		return q
	}(), ResultLiteral{"paid", "300"}),
	resultScenario("metric_filter_hidden_metric", CategoryFilter, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityMetricFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"orders_count"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "revenue", Operator: query.FilterGTE, Value: 100}}
		return q
	}(), ResultLiteral{"paid", "2"}),
	resultFixtureScenario(
		fixtures.DefinitionFilters,
		"metric_definition_filter_pre_aggregation",
		CategoryFilter,
		[]Capability{CapabilityAggregation, CapabilityRelationship, CapabilityDefinitionFilter},
		fixtureSemanticQuery(fixtures.DefinitionFilters, []string{"gold_revenue"}, nil),
		ResultLiteral{"175"},
	),
	resultFixtureScenario(
		fixtures.DefinitionFilters,
		"metric_definition_filter_post_aggregation",
		CategoryFilter,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityDerived, CapabilityDefinitionFilter},
		fixtureSemanticQuery(fixtures.DefinitionFilters, []string{"average_order_value"}, []query.DimensionRef{{Name: "segment"}}),
		ResultLiteral{"enterprise", "100"},
	),
}
