package scenarios

import (
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/query"
)

type Category string

const (
	CategoryMetric      Category = "metric"
	CategoryDimension   Category = "dimension"
	CategoryJoin        Category = "join"
	CategoryFilter      Category = "filter"
	CategoryQueryIntent Category = "query_intent"
	CategoryTime        Category = "time"
	CategoryComposition Category = "composition"
)

type Capability string

const (
	CapabilityAggregation                Capability = "aggregation"
	CapabilityDimension                  Capability = "dimension"
	CapabilityRelationship               Capability = "relationship"
	CapabilityFilter                     Capability = "filter"
	CapabilityOrdering                   Capability = "ordering"
	CapabilityTimeGrain                  Capability = "time_grain"
	CapabilityDerived                    Capability = "derived_metric"
	CapabilityRatio                      Capability = "ratio_metric"
	CapabilityMultiSource                Capability = "multi_source"
	CapabilityCumulative                 Capability = "cumulative_metric"
	CapabilityTimeOffset                 Capability = "time_offset_metric"
	CapabilityMetricFilter               Capability = "metric_filter"
	CapabilitySemiAdditive               Capability = "semi_additive_metric"
	CapabilitySemiAdditiveTieBreak       Capability = "semi_additive_tie_break"
	CapabilitySemiAdditiveNullSkip       Capability = "semi_additive_null_skip"
	CapabilitySemiAdditiveQueryGrain     Capability = "semi_additive_query_grain"
	CapabilitySemiAdditiveWindowGrouping Capability = "semi_additive_window_grouping"
	CapabilitySemiAdditiveRollup         Capability = "semi_additive_rollup"
	CapabilityDistinctValues             Capability = "distinct_values"
	CapabilityDefinitionFilter           Capability = "metric_definition_filter"
	CapabilityConversion                 Capability = "conversion_metric"
	CapabilityOffsetToGrain              Capability = "offset_to_grain"
	CapabilityCustomCalendar             Capability = "custom_calendar"
	CapabilityDenseCalendar              Capability = "dense_calendar"
)

type CapabilitySet map[Capability]struct{}

func Capabilities(values ...Capability) CapabilitySet {
	set := make(CapabilitySet, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}
func (set CapabilitySet) Supports(required []Capability) bool {
	for _, c := range required {
		if _, ok := set[c]; !ok {
			return false
		}
	}
	return true
}
func (set CapabilitySet) Missing(required []Capability) []Capability {
	var missing []Capability
	for _, c := range required {
		if _, ok := set[c]; !ok {
			missing = append(missing, c)
		}
	}
	return missing
}

// ContractStatus describes whether a correctness layer is an enforced part of
// the shared scenario contract or an explicitly recorded gap.
type ContractStatus string

const (
	ContractRequired ContractStatus = "required"
	ContractPending  ContractStatus = "pending"
)

// Verification records the correctness obligations for a shared scenario.
// Compiler status is intentionally explicit even though every Core scenario
// currently participates in compiler conformance: generated coverage must not
// infer support merely from the presence of implementation code.
type Verification struct {
	Compiler ContractStatus
	Result   ContractStatus
	Reason   string
}

// CompilerExpectation is the target-neutral physical-query shape shared by all
// formally registered compiler targets. Dialect-specific syntax remains in
// renderer contract tests.
type CompilerExpectation struct {
	Fragments  []string
	Parameters int
}

type Scenario struct {
	Name           string
	Category       Category
	Requires       []Capability
	Fixture        fixtures.ID
	Query          query.SemanticQuery
	Verification   Verification
	ExpectedQuery  CompilerExpectation
	ExpectedResult *ResultExpectation
}

const pendingResultReason = "result-level execution contract has not been migrated to the canonical corpus"

func compileScenario(name string, category Category, requires []Capability, q query.SemanticQuery) Scenario {
	return compileFixtureScenario(fixtures.Commerce, name, category, requires, q)
}

func compileFixtureScenario(fixture fixtures.ID, name string, category Category, requires []Capability, q query.SemanticQuery) Scenario {
	expectedQuery, ok := compilerExpectations[name]
	if !ok {
		panic("missing compiler expectation for shared scenario " + name)
	}
	return scenarioWithExpectation(fixture, name, category, requires, q, expectedQuery)
}

func scenarioWithExpectation(fixture fixtures.ID, name string, category Category, requires []Capability, q query.SemanticQuery, expectedQuery CompilerExpectation) Scenario {
	return Scenario{
		Name:          name,
		Category:      category,
		Requires:      requires,
		Fixture:       fixture,
		Query:         q,
		ExpectedQuery: expectedQuery,
		Verification: Verification{
			Compiler: ContractRequired,
			Result:   ContractPending,
			Reason:   pendingResultReason,
		},
	}
}

func resultScenario(name string, category Category, requires []Capability, q query.SemanticQuery, rows ...ResultLiteral) Scenario {
	return resultFixtureScenario(fixtures.Commerce, name, category, requires, q, rows...)
}

func resultFixtureScenario(fixture fixtures.ID, name string, category Category, requires []Capability, q query.SemanticQuery, rows ...ResultLiteral) Scenario {
	scenario := compileFixtureScenario(fixture, name, category, requires, q)
	scenario.Verification.Result = ContractRequired
	scenario.Verification.Reason = ""
	scenario.ExpectedResult = buildResultExpectation(fixture, q, rows)
	return scenario
}

func semanticQuery(metrics []string, dimensions []query.DimensionRef) query.SemanticQuery {
	return fixtureSemanticQuery(fixtures.Commerce, metrics, dimensions)
}

func fixtureSemanticQuery(fixture fixtures.ID, metrics []string, dimensions []query.DimensionRef) query.SemanticQuery {
	definition, ok := fixtures.Lookup(fixture)
	if !ok {
		panic("unknown canonical fixture " + string(fixture))
	}
	refs := make([]query.MetricRef, len(metrics))
	for i, n := range metrics {
		refs[i] = query.MetricRef{Name: n}
	}
	return query.SemanticQuery{Project: definition.Project, Model: definition.Model, Metrics: refs, Dimensions: dimensions}
}

// Core is assembled from stable semantic domains. Delivery-phase labels must
// not become part of the permanent scenario taxonomy.
var Core = concatScenarios(
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

func concatScenarios(groups ...[]Scenario) []Scenario {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	combined := make([]Scenario, 0, total)
	for _, group := range groups {
		combined = append(combined, group...)
	}
	return combined
}

func ByName(name string) (Scenario, bool) {
	for _, s := range Core {
		if s.Name == name {
			return s, true
		}
	}
	return Scenario{}, false
}
func ExecutionCore() []Scenario {
	var r []Scenario
	for _, s := range Core {
		if s.Verification.Result == ContractRequired {
			r = append(r, s)
		}
	}
	return r
}
func TimeGrain(name string, grain query.TimeGrain) Scenario {
	return scenarioWithExpectation(
		fixtures.Commerce,
		name,
		CategoryTime,
		[]Capability{CapabilityAggregation, CapabilityDimension, CapabilityTimeGrain},
		timeGrainQuery(grain),
		CompilerExpectation{Fragments: []string{"order_date", "GROUP BY"}},
	)
}

func timeGrainQuery(grain query.TimeGrain) query.SemanticQuery {
	return semanticQuery([]string{"revenue"}, []query.DimensionRef{{Name: "order_date", Grain: &grain}})
}
