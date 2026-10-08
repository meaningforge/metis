package scenarios

import "github.com/meaningforge/metis/query"

var compositionScenarios = []Scenario{
	resultScenario("derived_metric", CategoryComposition, []Capability{CapabilityDerived}, semanticQuery([]string{"contribution_margin"}, nil), ResultLiteral{"315"}),
	resultScenario("ratio_metric", CategoryComposition, []Capability{CapabilityRatio}, semanticQuery([]string{"average_order_value"}, nil), ResultLiteral{"116.66666666666667"}),
	resultScenario("nested_derived_metric", CategoryComposition, []Capability{CapabilityDerived, CapabilityRatio}, semanticQuery([]string{"nested_margin_ratio"}, nil), ResultLiteral{"0.9"}),
	resultScenario("multi_source_derived_at_grain", CategoryComposition, []Capability{CapabilityDerived, CapabilityMultiSource, CapabilityTimeGrain}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"gross_margin"}, []query.DimensionRef{{Name: "calendar.day", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", "180"}, ResultLiteral{"2026-02-01", "30"}),
	resultScenario("independent_multi_source_at_grain", CategoryComposition, []Capability{CapabilityMultiSource, CapabilityTimeGrain}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"revenue", "cost"}, []query.DimensionRef{{Name: "calendar.day", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", "300", "120"}, ResultLiteral{"2026-02-01", "50", "20"}),
	resultScenario("independent_multi_source_ungrouped", CategoryComposition, []Capability{CapabilityMultiSource}, semanticQuery([]string{"revenue", "cost"}, nil), ResultLiteral{"350", "140"}),
	resultScenario("cumulative_metric_by_month_and_region", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship, CapabilityTimeGrain, CapabilityCumulative}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}, {Name: "region"}})
	}(), ResultLiteral{"2026-01-01", "APAC", "300"}, ResultLiteral{"2026-02-01", "EU", "50"}),
	resultScenario("cumulative_metric_by_quarter_and_region", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship, CapabilityTimeGrain, CapabilityCumulative}, func() query.SemanticQuery {
		grain := query.TimeGrainQuarter
		return semanticQuery([]string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}, {Name: "region"}})
	}(), ResultLiteral{"2026-01-01", "APAC", "300"}, ResultLiteral{"2026-01-01", "EU", "50"}),
	resultScenario("time_offset_current_and_previous_by_region", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship, CapabilityTimeGrain, CapabilityTimeOffset}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"revenue", "previous_month_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}, {Name: "region"}})
	}(), ResultLiteral{"2026-01-01", "APAC", "300", NullValue}, ResultLiteral{"2026-02-01", "APAC", NullValue, "300"}, ResultLiteral{"2026-02-01", "EU", "50", NullValue}, ResultLiteral{"2026-03-01", "EU", NullValue, "50"}),
	resultScenario("time_offset_period_over_period_growth", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset, CapabilityDerived}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		return semanticQuery([]string{"revenue_growth_rate"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
	}(), ResultLiteral{"2026-01-01", NullValue}, ResultLiteral{"2026-02-01", "-0.8333333333333334"}, ResultLiteral{"2026-03-01", NullValue}),
	resultScenario("metric_filter_derived_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityDerived, CapabilityMetricFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "contribution_margin", Operator: query.FilterBetween, Value: []int{100, 10000}}}
		return q
	}(), ResultLiteral{"paid", "300"}),
	resultScenario("metric_filter_time_offset_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityTimeOffset, CapabilityMetricFilter}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
		q.Filters = []query.Filter{{Field: "previous_month_revenue", Operator: query.FilterGT, Value: 0}}
		return q
	}(), ResultLiteral{"2026-02-01", "50"}),
	resultScenario("metric_filter_with_dimension_filter", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityMetricFilter}, func() query.SemanticQuery {
		q := semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "status"}})
		q.Filters = []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}, {Field: "revenue", Operator: query.FilterGT, Value: 100}}
		return q
	}(), ResultLiteral{"paid", "300"}),
	resultScenario("metric_filter_cumulative_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityCumulative, CapabilityMetricFilter}, func() query.SemanticQuery {
		grain := query.TimeGrainMonth
		q := semanticQuery([]string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
		q.Filters = []query.Filter{{Field: "cumulative_revenue", Operator: query.FilterGT, Value: 300}}
		return q
	}(), ResultLiteral{"2026-02-01", "350"}),
}
