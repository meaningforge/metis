package scenarios

import "github.com/meaningforge/metis/query"

var metricScenarios = []Scenario{
	resultScenario("simple_metric", CategoryMetric, []Capability{CapabilityAggregation}, semanticQuery([]string{"revenue"}, nil), ResultLiteral{"350"}),
	resultScenario("multiple_metrics_same_source", CategoryMetric, []Capability{CapabilityAggregation}, semanticQuery([]string{"revenue", "orders_count"}, nil), ResultLiteral{"350", "3"}),
	resultScenario("aggregation_variants", CategoryMetric, []Capability{CapabilityAggregation}, semanticQuery([]string{"average_order_amount", "minimum_order_amount", "maximum_order_amount"}, nil), ResultLiteral{"116.66666666666667", "50", "200"}),
	resultScenario("order_by_metric_ungrouped", CategoryMetric, []Capability{CapabilityAggregation, CapabilityOrdering}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, nil)
		q.OrderBy = []query.OrderBy{{Field: "revenue", Direction: query.SortDesc}}
		return q
	}(), ResultLiteral{"350"}),
}
