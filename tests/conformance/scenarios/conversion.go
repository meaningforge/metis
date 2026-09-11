package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

var conversionScenarios = []Scenario{
	resultFixtureScenario(
		fixtures.Conversion,
		"conversion_rate_by_campaign",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityConversion},
		fixtureSemanticQuery(fixtures.Conversion, []string{"signup_to_purchase_rate"}, []query.DimensionRef{{Name: "campaign"}}),
		ResultLiteral{"A", "0.25"},
		ResultLiteral{"B", "1"},
	),
	resultFixtureScenario(
		fixtures.Conversion,
		"conversion_count_by_campaign",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityConversion},
		fixtureSemanticQuery(fixtures.Conversion, []string{"signup_conversions"}, []query.DimensionRef{{Name: "campaign"}}),
		ResultLiteral{"A", "1"},
		ResultLiteral{"B", "1"},
	),
	resultFixtureScenario(
		fixtures.Conversion,
		"conversion_rate_filtered_campaign",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityConversion},
		filteredConversionQuery("signup_to_purchase_rate"),
		ResultLiteral{"A", "0.25"},
	),
	resultFixtureScenario(
		fixtures.Conversion,
		"conversion_count_filtered_campaign",
		CategoryComposition,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityFilter, CapabilityConversion},
		filteredConversionQuery("signup_conversions"),
		ResultLiteral{"A", "1"},
	),
}

func filteredConversionQuery(metric string) query.SemanticQuery {
	q := fixtureSemanticQuery(fixtures.Conversion, []string{metric}, []query.DimensionRef{{Name: "campaign"}})
	q.Filters = []query.Filter{{Field: "campaign", Operator: query.FilterEQ, Value: "A"}}
	return q
}
