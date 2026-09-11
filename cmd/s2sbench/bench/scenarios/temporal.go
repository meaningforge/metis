package scenarios

import (
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/query"
)

const CapabilityTemporalRelationship Capability = "temporal_relationship"

var temporalScenarios = []Scenario{
	temporalResultScenario(
		"temporal_join_point_in_time",
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityRelationship, CapabilityTemporalRelationship},
		fixtureSemanticQuery(fixtures.TemporalRelationship, []string{"revenue"}, []query.DimensionRef{{Name: "customer_tier"}}),
		CompilerExpectation{Fragments: []string{"customer_history", "order_time", "valid_from", "valid_to", "COALESCE", ">=", "<"}},
		ResultLiteral{"bronze", "10"},
		ResultLiteral{"gold", "50"},
		ResultLiteral{"silver", "40"},
	),
	temporalResultScenario(
		"temporal_join_half_open_boundary",
		[]Capability{CapabilityDimension, CapabilityRelationship, CapabilityFilter, CapabilityTemporalRelationship},
		func() query.SemanticQuery {
			q := fixtureSemanticQuery(fixtures.TemporalRelationship, nil, []query.DimensionRef{{Name: "order_id"}, {Name: "customer_tier"}})
			q.Filters = []query.Filter{{Field: "order_id", Operator: query.FilterEQ, Value: "o_boundary"}}
			return q
		}(),
		CompilerExpectation{Fragments: []string{"customer_history", "order_time", "valid_from", "valid_to", "COALESCE", "WHERE"}, Parameters: 1},
		ResultLiteral{"o_boundary", "gold"},
	),
	temporalResultScenario(
		"temporal_join_open_ended_version",
		[]Capability{CapabilityDimension, CapabilityRelationship, CapabilityFilter, CapabilityTemporalRelationship},
		func() query.SemanticQuery {
			q := fixtureSemanticQuery(fixtures.TemporalRelationship, nil, []query.DimensionRef{{Name: "order_id"}, {Name: "customer_tier"}})
			q.Filters = []query.Filter{{Field: "order_id", Operator: query.FilterEQ, Value: "o_current"}}
			return q
		}(),
		CompilerExpectation{Fragments: []string{"customer_history", "order_time", "valid_from", "valid_to", "COALESCE", "WHERE"}, Parameters: 1},
		ResultLiteral{"o_current", "gold"},
	),
	temporalResultScenario(
		"temporal_join_reverse_traversal",
		[]Capability{CapabilityDimension, CapabilityRelationship, CapabilityTemporalRelationship},
		fixtureSemanticQuery(fixtures.TemporalRelationship, nil, []query.DimensionRef{{Name: "customer_tier"}, {Name: "status"}}),
		CompilerExpectation{Fragments: []string{"customer_history", "orders", "order_time", "valid_from", "valid_to", "COALESCE"}},
		ResultLiteral{"bronze", "paid"},
		ResultLiteral{"gold", "paid"},
		ResultLiteral{"silver", "refunded"},
	),
}

func init() {
	for _, scenario := range temporalScenarios {
		compilerExpectations[scenario.Name] = scenario.ExpectedQuery
	}
	Core = append(Core, temporalScenarios...)
}

func temporalResultScenario(name string, requires []Capability, q query.SemanticQuery, expectedQuery CompilerExpectation, rows ...ResultLiteral) Scenario {
	scenario := scenarioWithExpectation(fixtures.TemporalRelationship, name, CategoryJoin, requires, q, expectedQuery)
	scenario.Verification.Result = ContractRequired
	scenario.Verification.Reason = ""
	scenario.ExpectedResult = buildResultExpectation(fixtures.TemporalRelationship, q, rows)
	return scenario
}
