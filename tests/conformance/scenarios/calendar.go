package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

var calendarScenarios = []Scenario{
	resultFixtureScenario(
		fixtures.OffsetToGrain,
		"offset_to_grain_year_start_by_month_region",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityOffsetToGrain},
		func() query.SemanticQuery {
			month := query.TimeGrainMonth
			return fixtureSemanticQuery(fixtures.OffsetToGrain, []string{"revenue_at_year_start"}, []query.DimensionRef{{Name: "order_date", Grain: &month}, {Name: "region"}})
		}(),
		ResultLiteral{"2026-01-01", "east", "10"},
		ResultLiteral{"2026-02-01", "east", "10"},
		ResultLiteral{"2026-03-01", "east", "10"},
		ResultLiteral{"2027-01-01", "east", "40"},
		ResultLiteral{"2027-02-01", "east", "40"},
		ResultLiteral{"2026-01-01", "west", "7"},
		ResultLiteral{"2026-02-01", "west", "7"},
	),
	resultFixtureScenario(
		fixtures.OffsetToGrainDense,
		"offset_to_grain_missing_boundary_stays_zero",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain, CapabilityOffsetToGrain, CapabilityDenseCalendar},
		customGrainQuery(fixtures.OffsetToGrainDense, "revenue_at_year_start", "order_date", query.TimeGrainMonth),
		ResultLiteral{"2026-01-01", "0"},
		ResultLiteral{"2026-02-01", "0"},
		ResultLiteral{"2026-03-01", "0"},
	),
	resultFixtureScenario(
		fixtures.CustomOffsetToGrainDense,
		"custom_offset_to_grain_missing_boundary_stays_zero",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityOffsetToGrain, CapabilityCustomCalendar, CapabilityDenseCalendar},
		customGrainQuery(fixtures.CustomOffsetToGrainDense, "revenue_at_fiscal_year_start", "day", query.TimeGrain("fiscal_week")),
		ResultLiteral{"2026-01-05", "0"},
		ResultLiteral{"2026-01-12", "0"},
		ResultLiteral{"2026-01-19", "0"},
	),
	resultFixtureScenario(
		fixtures.CustomCalendarOffset,
		"custom_calendar_previous_fiscal_week",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeOffset, CapabilityCustomCalendar},
		customGrainQuery(fixtures.CustomCalendarOffset, "previous_fiscal_week_revenue", "day", query.TimeGrain("fiscal_week")),
		ResultLiteral{"2026-01-12", "15"},
		ResultLiteral{"2026-01-19", "22"},
	),
	resultFixtureScenario(
		fixtures.CustomCalendarDense,
		"custom_calendar_dense_missing_period",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeOffset, CapabilityCustomCalendar, CapabilityDenseCalendar},
		customGrainQuery(fixtures.CustomCalendarDense, "previous_fiscal_week_revenue", "calendar_day", query.TimeGrain("fiscal_week")),
		ResultLiteral{"2026-01-12", "15"},
		ResultLiteral{"2026-01-19", "0"},
	),
	resultFixtureScenario(
		fixtures.CustomCalendarRolling,
		"custom_calendar_rolling_three_fiscal_weeks",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityCumulative, CapabilityCustomCalendar, CapabilityDenseCalendar},
		customGrainQuery(fixtures.CustomCalendarRolling, "rolling_3_fiscal_week_revenue", "calendar_day", query.TimeGrain("fiscal_week")),
		ResultLiteral{"2026-01-05", "10"},
		ResultLiteral{"2026-01-12", "10"},
		ResultLiteral{"2026-01-19", "40"},
		ResultLiteral{"2026-01-26", "70"},
	),
	resultFixtureScenario(
		fixtures.CustomCalendarGrainToDate,
		"custom_calendar_fiscal_quarter_to_date",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityCumulative, CapabilityCustomCalendar, CapabilityDenseCalendar},
		customGrainQuery(fixtures.CustomCalendarGrainToDate, "fiscal_quarter_to_date_revenue", "calendar_day", query.TimeGrain("fiscal_week")),
		ResultLiteral{"2026-01-05", "10"},
		ResultLiteral{"2026-01-12", "30"},
		ResultLiteral{"2026-04-06", "30"},
		ResultLiteral{"2026-04-13", "70"},
	),
	resultFixtureScenario(
		fixtures.CustomCalendarSemiAdditive,
		"custom_calendar_semi_additive_last_snapshot",
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilitySemiAdditive, CapabilitySemiAdditiveQueryGrain, CapabilityCustomCalendar},
		customGrainQuery(fixtures.CustomCalendarSemiAdditive, "inventory_balance", "snapshot_date", query.TimeGrain("fiscal_week")),
		ResultLiteral{"2026-01-05", "13"},
		ResultLiteral{"2026-01-12", "25"},
	),
}

func customGrainQuery(fixture fixtures.ID, metric, dimension string, grain query.TimeGrain) query.SemanticQuery {
	return fixtureSemanticQuery(fixture, []string{metric}, []query.DimensionRef{{Name: dimension, Grain: &grain}})
}
