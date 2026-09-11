package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

var semiAdditiveScenarios = []Scenario{
	resultScenario("semi_additive_latest_snapshot", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive}, semanticQuery([]string{"inventory_balance"}, []query.DimensionRef{{Name: "warehouse"}}), ResultLiteral{"tokyo", "40"}, ResultLiteral{"osaka", "15"}),
	resultScenario("semi_additive_as_of_time", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilitySemiAdditive}, func() query.SemanticQuery {
		q := semanticQuery([]string{"inventory_balance"}, []query.DimensionRef{{Name: "warehouse"}})
		q.Filters = []query.Filter{{Field: "snapshot_date", Operator: query.FilterLTE, Value: "2026-06-30"}}
		return q
	}(), ResultLiteral{"tokyo", "30"}, ResultLiteral{"osaka", "12"}),
	resultScenario("semi_additive_as_of_with_dimension_filter", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilitySemiAdditive}, func() query.SemanticQuery {
		q := semanticQuery([]string{"inventory_balance"}, []query.DimensionRef{{Name: "warehouse"}})
		q.Filters = []query.Filter{{Field: "snapshot_date", Operator: query.FilterLTE, Value: "2026-06-30"}, {Field: "warehouse", Operator: query.FilterEQ, Value: "tokyo"}}
		return q
	}(), ResultLiteral{"tokyo", "30"}),
	resultScenario("semi_additive_since_time", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilitySemiAdditive}, func() query.SemanticQuery {
		q := semanticQuery([]string{"inventory_balance"}, []query.DimensionRef{{Name: "warehouse"}})
		q.Filters = []query.Filter{{Field: "snapshot_date", Operator: query.FilterGTE, Value: "2026-01-01"}}
		return q
	}(), ResultLiteral{"tokyo", "40"}, ResultLiteral{"osaka", "15"}),
	resultScenario("semi_additive_between_time", CategoryComposition, []Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilitySemiAdditive}, func() query.SemanticQuery {
		q := semanticQuery([]string{"inventory_balance"}, []query.DimensionRef{{Name: "warehouse"}})
		q.Filters = []query.Filter{{Field: "snapshot_date", Operator: query.FilterBetween, Value: []string{"2026-01-01", "2026-06-30"}}}
		return q
	}(), ResultLiteral{"tokyo", "30"}, ResultLiteral{"osaka", "12"}),
	resultFixtureScenario(
		fixtures.SemiAdditiveTieBreak,
		"semi_additive_first_with_tie_break",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive, CapabilitySemiAdditiveTieBreak},
		fixtureSemanticQuery(fixtures.SemiAdditiveTieBreak, []string{"inventory_first_tie_break"}, []query.DimensionRef{{Name: "warehouse"}}),
		ResultLiteral{"w1", "10"},
		ResultLiteral{"w2", "5"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveTieBreak,
		"semi_additive_last_with_tie_break",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive, CapabilitySemiAdditiveTieBreak},
		fixtureSemanticQuery(fixtures.SemiAdditiveTieBreak, []string{"inventory_last_tie_break"}, []query.DimensionRef{{Name: "warehouse"}}),
		ResultLiteral{"w1", "40"},
		ResultLiteral{"w2", "25"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveNullSkip,
		"semi_additive_first_skip_null",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive, CapabilitySemiAdditiveTieBreak, CapabilitySemiAdditiveNullSkip},
		fixtureSemanticQuery(fixtures.SemiAdditiveNullSkip, []string{"inventory_first_skip_null"}, []query.DimensionRef{{Name: "warehouse"}}),
		ResultLiteral{"w1", "10"},
		ResultLiteral{"w2", "5"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveNullSkip,
		"semi_additive_last_skip_null",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive, CapabilitySemiAdditiveTieBreak, CapabilitySemiAdditiveNullSkip},
		fixtureSemanticQuery(fixtures.SemiAdditiveNullSkip, []string{"inventory_last_skip_null"}, []query.DimensionRef{{Name: "warehouse"}}),
		ResultLiteral{"w1", "20"},
		ResultLiteral{"w2", "5"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveQueriedWindow,
		"semi_additive_queried_week",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilitySemiAdditive, CapabilitySemiAdditiveQueryGrain},
		func() query.SemanticQuery {
			week := query.TimeGrainWeek
			return fixtureSemanticQuery(fixtures.SemiAdditiveQueriedWindow, []string{"inventory_balance"}, []query.DimensionRef{{Name: "snapshot_date", Grain: &week}})
		}(),
		ResultLiteral{"2026-01-05", "13"},
		ResultLiteral{"2026-01-12", "25"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveWindowGrouping,
		"semi_additive_window_group_sum",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilitySemiAdditive, CapabilitySemiAdditiveWindowGrouping, CapabilitySemiAdditiveRollup},
		fixtureSemanticQuery(fixtures.SemiAdditiveWindowGrouping, []string{"inventory_window_sum"}, nil),
		ResultLiteral{"33"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveWindowGrouping,
		"semi_additive_window_group_min",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilitySemiAdditive, CapabilitySemiAdditiveWindowGrouping, CapabilitySemiAdditiveRollup},
		fixtureSemanticQuery(fixtures.SemiAdditiveWindowGrouping, []string{"inventory_window_min"}, nil),
		ResultLiteral{"11"},
	),
	resultFixtureScenario(
		fixtures.SemiAdditiveWindowGrouping,
		"semi_additive_window_group_max",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilitySemiAdditive, CapabilitySemiAdditiveWindowGrouping, CapabilitySemiAdditiveRollup},
		fixtureSemanticQuery(fixtures.SemiAdditiveWindowGrouping, []string{"inventory_window_max"}, nil),
		ResultLiteral{"22"},
	),
}
