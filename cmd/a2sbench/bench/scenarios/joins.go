package scenarios

import "github.com/meaningforge/metis/query"

var joinScenarios = []Scenario{
	resultScenario("one_hop_join", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship}, semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "region"}}), ResultLiteral{"APAC", "300"}, ResultLiteral{"EU", "50"}),
	resultScenario("multi_hop_join", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship}, semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "country"}}), ResultLiteral{"JP", "300"}, ResultLiteral{"DE", "50"}),
	resultScenario("multiple_joined_dimensions", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship}, semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "country"}, {Name: "category"}}), ResultLiteral{"JP", "electronics", "100"}, ResultLiteral{"JP", "apparel", "200"}, ResultLiteral{"DE", "electronics", "50"}),
	resultScenario("cross_dataset_leaf_metric", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship}, semanticQuery([]string{"revenue_per_customer"}, []query.DimensionRef{{Name: "region"}}), ResultLiteral{"APAC", "150"}, ResultLiteral{"EU", "50"}),
	resultScenario("metric_local_and_joined_dimensions", CategoryJoin, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship}, semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}, {Name: "region"}}), ResultLiteral{"paid", "APAC", "300"}, ResultLiteral{"refunded", "EU", "50"}),
	resultScenario("multiple_metrics_joined_dimension", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship}, semanticQuery([]string{"revenue", "orders_count"}, []query.DimensionRef{{Name: "region"}}), ResultLiteral{"APAC", "300", "2"}, ResultLiteral{"EU", "50", "1"}),
	resultScenario("joined_dimension_filter_order_limit", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship, CapabilityFilter, CapabilityOrdering}, func() query.SemanticQuery {
		limit := 10
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "region"}})
		q.Filters = []query.Filter{{Field: "region", Operator: query.FilterNEQ, Value: "UNKNOWN"}}
		q.OrderBy = []query.OrderBy{{Field: "revenue", Direction: query.SortDesc}}
		q.Limit = &limit
		return q
	}(), ResultLiteral{"APAC", "300"}, ResultLiteral{"EU", "50"}),
	resultScenario("multi_hop_intermediate_filter", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship, CapabilityFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "country"}})
		q.Filters = []query.Filter{{Field: "segment", Operator: query.FilterEQ, Value: "enterprise"}}
		return q
	}(), ResultLiteral{"JP", "100"}),
	resultScenario("multi_hop_terminal_filter", CategoryJoin, []Capability{CapabilityAggregation, CapabilityRelationship, CapabilityFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "country", Operator: query.FilterEQ, Value: "JP"}}
		return q
	}(), ResultLiteral{"paid", "300"}),
}
