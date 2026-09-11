package scenarios

import "github.com/meaningforge/metis/query"

var timeScenarios = []Scenario{
	resultScenario("time_month", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain}, timeGrainQuery(query.TimeGrainMonth), ResultLiteral{"2026-01-01", "300"}, ResultLiteral{"2026-02-01", "50"}),
	resultScenario("time_filter_with_month_grain", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityTimeGrain}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
		q.Filters = []query.Filter{{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-01-01", "2026-03-31"}}}
		return q
	}(), ResultLiteral{"2026-01-01", "300"}, ResultLiteral{"2026-02-01", "50"}),
	resultScenario("multiple_metrics_time_grain", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"revenue", "orders_count"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", "300", "2"}, ResultLiteral{"2026-02-01", "50", "1"}),
	resultScenario("cumulative_metric_by_month", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityCumulative}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", "300"}, ResultLiteral{"2026-02-01", "350"}),
	resultScenario("cumulative_metric_by_quarter", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityCumulative}, func() query.SemanticQuery {
		grain := query.TimeGrainQuarter
		return semanticQuery([]string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", "350"}),
	resultScenario("time_offset_previous_month", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"previous_month_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-02-01", "300"}, ResultLiteral{"2026-03-01", "50"}),
	resultScenario("time_offset_previous_quarter", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset}, func() query.SemanticQuery {
		grain := query.TimeGrainQuarter
		return semanticQuery([]string{"previous_quarter_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-04-01", "350"}),
	resultScenario("time_offset_previous_year", CategoryTime, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset}, func() query.SemanticQuery {
		grain := query.TimeGrainYear
		return semanticQuery([]string{"previous_year_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2027-01-01", "350"}),
}
