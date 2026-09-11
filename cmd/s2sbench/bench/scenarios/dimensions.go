package scenarios

import "github.com/meaningforge/metis/query"

var dimensionScenarios = []Scenario{
	resultScenario("local_dimension", CategoryDimension, []Capability{CapabilityAggregation, CapabilityDimension}, semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}}), ResultLiteral{"paid", "300"}, ResultLiteral{"refunded", "50"}),
	resultScenario("dimensions_only", CategoryDimension, []Capability{CapabilityDimension, CapabilityRelationship}, semanticQuery(nil, []query.DimensionRef{{Name: "status"}, {Name: "region"}}), ResultLiteral{"paid", "APAC"}, ResultLiteral{"refunded", "EU"}),
	resultScenario("multiple_local_dimensions", CategoryDimension, []Capability{CapabilityAggregation, CapabilityDimension}, semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}, {Name: "order_date"}}), ResultLiteral{"paid", "2026-01-15", "100"}, ResultLiteral{"paid", "2026-01-20", "200"}, ResultLiteral{"refunded", "2026-02-10", "50"}),
	resultScenario("limit_without_order_by", CategoryDimension, []Capability{CapabilityAggregation, CapabilityDimension}, func() query.SemanticQuery {
		limit := 5
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Limit = &limit
		return q
	}(), ResultLiteral{"paid", "300"}, ResultLiteral{"refunded", "50"}),
	resultScenario("dimensions_only_filter_order_limit", CategoryDimension, []Capability{CapabilityDimension, CapabilityRelationship, CapabilityFilter, CapabilityOrdering}, func() query.SemanticQuery {
		limit := 20
		q := semanticQuery(nil, []query.DimensionRef{{Name: "status"}, {Name: "region"}})
		q.Filters = []query.Filter{{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
		q.OrderBy = []query.OrderBy{{Field: "status", Direction: query.SortAsc}}
		q.Limit = &limit
		return q
	}(), ResultLiteral{"paid", "APAC"}),
}
