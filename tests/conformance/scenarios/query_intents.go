package scenarios

import (
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

var queryIntentScenarios = []Scenario{
	resultFixtureScenario(
		fixtures.DistinctValues,
		"distinct_dimension_values",
		CategoryQueryIntent,
		[]Capability{CapabilityDistinctValues, CapabilityDimension, CapabilityFilter, CapabilityOrdering},
		func() query.SemanticQuery {
			limit := 10
			q := fixtureSemanticQuery(fixtures.DistinctValues, nil, []query.DimensionRef{{Name: "region"}})
			q.Intent = query.QueryIntentDistinctValues
			q.Filters = []query.Filter{{Field: "country", Operator: query.FilterEQ, Value: "JP"}}
			q.OrderBy = []query.OrderBy{{Field: "region", Direction: query.SortAsc}}
			q.Limit = &limit
			return q
		}(),
		ResultLiteral{"kansai"},
		ResultLiteral{"kanto"},
	),
}
