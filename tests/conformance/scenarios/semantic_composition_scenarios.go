package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

var semanticCompositionScenarios = func() []Scenario {
	registerSemanticCompositionCompilerExpectations()
	return []Scenario{
		resultScenario("cumulative_metric_with_source_filter", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityTimeGrain, CapabilityCumulative}, func() query.SemanticQuery {
			grain := query.TimeGrainMonth
			q := semanticQuery([]string{"cumulative_revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
			q.Filters = []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}}
			return q
		}(), ResultLiteral{"2026-01-01", "300"}),
		resultScenario("semi_additive_with_ordinary_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive}, semanticQuery([]string{"inventory_balance", "inventory_quantity"}, []query.DimensionRef{{Name: "warehouse"}}), ResultLiteral{"tokyo", "40", "100"}, ResultLiteral{"osaka", "15", "40"}),
		resultScenario("shared_grain_derived_with_source_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityDerived}, semanticQuery([]string{"revenue", "contribution_margin"}, []query.DimensionRef{{Name: "status"}}), ResultLiteral{"paid", "300", "270"}, ResultLiteral{"refunded", "50", "45"}),
		resultScenario("metric_filter_multi_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityDerived, CapabilityMetricFilter}, func() query.SemanticQuery {
			q := semanticQuery([]string{"revenue", "orders_count"}, []query.DimensionRef{{Name: "status"}})
			q.Filters = []query.Filter{{Field: "contribution_margin", Operator: query.FilterGT, Value: 100}}
			return q
		}(), ResultLiteral{"paid", "300", "2"}),
		resultScenario("semantic_extension_derived_metric", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDerived}, semanticQuery([]string{"scaled_net_revenue"}, nil), ResultLiteral{"665"}),
	}
}()

var semanticCalendarCompositionScenarios = []Scenario{
	resultFixtureScenario(
		fixtures.CustomCalendarRolling,
		"custom_calendar_cumulative_with_filter",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityCumulative, CapabilityCustomCalendar, CapabilityDenseCalendar},
		func() query.SemanticQuery {
			q := customGrainQuery(fixtures.CustomCalendarRolling, "rolling_3_fiscal_week_revenue", "calendar_day", query.TimeGrain("fiscal_week"))
			q.Filters = []query.Filter{{Field: "rolling_3_fiscal_week_revenue", Operator: query.FilterGT, Value: 10}}
			return q
		}(),
		ResultLiteral{"2026-01-19", "40"},
		ResultLiteral{"2026-01-26", "70"},
	),
}

func init() {
	compositionScenarios = append(compositionScenarios, semanticCompositionScenarios...)
	calendarScenarios = append(calendarScenarios, semanticCalendarCompositionScenarios...)
	Core = concatScenarios(
		metricScenarios,
		dimensionScenarios,
		joinScenarios,
		filterScenarios,
		queryIntentScenarios,
		timeScenarios,
		calendarScenarios,
		compositionScenarios,
		conversionScenarios,
		semiAdditiveScenarios,
		dataEdgeScenarios,
	)
}
