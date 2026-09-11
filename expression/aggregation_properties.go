package expression

import "strings"

// Aggregation algebra derivation. The aggregation algebra keeps two properties orthogonal
// because they disagree in practice: COUNT over a DISTINCT argument is
// duplicate-invariant and rollup-holistic at the same time.
//
// Derivation only. Nothing here decides fan-out safety, rollup legality, or any
// other planner behavior; properties are evidence for later phases to consume.

// DuplicateSensitivity records whether an aggregation is invariant under
// arbitrary per-row multiplicity: replacing each input row with m_i identical
// copies, for any m_i >= 1 chosen independently per row.
//
// Uniform replication is a strictly weaker condition and is not what a fan-out
// join produces, so classification is defined over the general case. AVG is
// therefore Sensitive: varying multiplicity reweights the mean even though a
// uniformly replicated input would leave it unchanged.
//
// Row elimination is deliberately out of scope. That is a change of population
// rather than duplication.
type DuplicateSensitivity string

const (
	DuplicateSensitive DuplicateSensitivity = "SENSITIVE"
	DuplicateInvariant DuplicateSensitivity = "INVARIANT"
	DuplicateUnknown   DuplicateSensitivity = "UNKNOWN"
)

// RollupAlgebra records whether a coarser-grain result can be computed from
// finer-grain partial results.
type RollupAlgebra string

const (
	RollupDistributive RollupAlgebra = "DISTRIBUTIVE"
	RollupAlgebraic    RollupAlgebra = "ALGEBRAIC"
	RollupHolistic     RollupAlgebra = "HOLISTIC"
	RollupUnknown      RollupAlgebra = "UNKNOWN"
)

// FinalizeKind is the step applied to merged partial state to produce the
// aggregation's value.
type FinalizeKind string

const (
	FinalizeIdentity FinalizeKind = "IDENTITY"
	FinalizeRatio    FinalizeKind = "RATIO"
	FinalizeNone     FinalizeKind = "NONE"
)

// Partial-state component names. AVG carries two; every other modelled
// aggregation carries one.
const (
	PartialStateSum   = "sum"
	PartialStateCount = "count"
	PartialStateValue = "value"
)

// PartialStateComponent is one accumulated value that a rollup must merge.
//
// Predicated records that the component is accumulated under the aggregation's
// own predicate. The predicate belongs to the partial state, not to the merge
// operator: SUMIF accumulates a conditional sum and merges those conditional
// sums with plain SUM. Re-applying the predicate while merging would filter
// values that were already accumulated under it.
type PartialStateComponent struct {
	Name       string
	Predicated bool
}

// AggregationProperties is the derived algebra of one aggregation call site.
//
// Derived distinguishes a resolved property set from a fail-closed Unknown. An
// aggregation Metis does not model, including a target-native function reaching
// the analyzer through AllowUnknownFunctions, yields Unknown on both axes rather
// than a default.
type AggregationProperties struct {
	Function     string
	Duplicate    DuplicateSensitivity
	Rollup       RollupAlgebra
	PartialState []PartialStateComponent
	Merge        string
	Finalize     FinalizeKind
	Derived      bool
}

// UnknownAggregationProperties is the fail-closed value for a call site whose
// algebra cannot be derived.
func UnknownAggregationProperties(name string) AggregationProperties {
	return AggregationProperties{
		Function:  name,
		Duplicate: DuplicateUnknown,
		Rollup:    RollupUnknown,
		Merge:     "",
		Finalize:  FinalizeNone,
		Derived:   false,
	}
}

// modelledAggregations is the property table for aggregations Metis models.
//
// COUNT merges through SUM rather than through COUNT. That is the canonical
// trap this table exists to prevent: merging counts by counting them would
// return the number of partials instead of the number of rows.
var modelledAggregations = map[string]AggregationProperties{
	"SUM": {
		Duplicate:    DuplicateSensitive,
		Rollup:       RollupDistributive,
		PartialState: []PartialStateComponent{{Name: PartialStateSum}},
		Merge:        "SUM",
		Finalize:     FinalizeIdentity,
	},
	"SUMIF": {
		Duplicate:    DuplicateSensitive,
		Rollup:       RollupDistributive,
		PartialState: []PartialStateComponent{{Name: PartialStateSum, Predicated: true}},
		Merge:        "SUM",
		Finalize:     FinalizeIdentity,
	},
	"COUNT": {
		Duplicate:    DuplicateSensitive,
		Rollup:       RollupDistributive,
		PartialState: []PartialStateComponent{{Name: PartialStateCount}},
		Merge:        "SUM",
		Finalize:     FinalizeIdentity,
	},
	"MIN": {
		Duplicate:    DuplicateInvariant,
		Rollup:       RollupDistributive,
		PartialState: []PartialStateComponent{{Name: PartialStateValue}},
		Merge:        "MIN",
		Finalize:     FinalizeIdentity,
	},
	"MAX": {
		Duplicate:    DuplicateInvariant,
		Rollup:       RollupDistributive,
		PartialState: []PartialStateComponent{{Name: PartialStateValue}},
		Merge:        "MAX",
		Finalize:     FinalizeIdentity,
	},
	"AVG": {
		Duplicate:    DuplicateSensitive,
		Rollup:       RollupAlgebraic,
		PartialState: []PartialStateComponent{{Name: PartialStateSum}, {Name: PartialStateCount}},
		Merge:        "SUM",
		Finalize:     FinalizeRatio,
	},
}

// DeriveAggregationProperties resolves the algebra of one function call site.
//
// The second result reports whether the call site carries aggregation
// properties at all. A function the registry knows to be scalar does not, and
// is skipped rather than being recorded as Unknown. A function the registry
// does not know does, because Metis cannot rule out that it aggregates.
func DeriveAggregationProperties(registry *FunctionRegistry, call *FunctionCallExpr) (AggregationProperties, bool) {
	if call == nil {
		return AggregationProperties{}, false
	}
	if registry == nil {
		registry = DefaultFunctionRegistry()
	}
	name := strings.ToUpper(strings.TrimSpace(call.Name))

	signatures := registry.Signatures(name)
	if len(signatures) == 0 {
		// Unresolved, including target-native aggregations such as uniqExact or
		// quantile. The analyzer types these as scalar by default; that default
		// is a placeholder rather than a claim, so it must not be read as one.
		return UnknownAggregationProperties(call.Name), true
	}
	if !anyAggregateSignature(signatures) {
		return AggregationProperties{}, false
	}

	base, ok := modelledAggregations[name]
	if !ok {
		// Registered as an aggregate but absent from the property table. Fail
		// closed instead of assuming an algebra.
		return UnknownAggregationProperties(call.Name), true
	}
	base.Function = call.Name
	base.Derived = true
	base.PartialState = append([]PartialStateComponent(nil), base.PartialState...)

	if hasDistinctArgument(call) {
		base = applyDistinct(base)
	}
	return base, true
}

// applyDistinct adjusts a modelled aggregation for a DISTINCT argument.
//
// Deduplication absorbs any per-row multiplicity, so the aggregation becomes
// duplicate-invariant regardless of what it was. Rollup is a separate question:
// an additive merge over distinct partials would double-count values present in
// more than one partial, so additively merged aggregations become holistic.
// Extremum merges are unaffected because MIN and MAX ignore multiplicity.
func applyDistinct(props AggregationProperties) AggregationProperties {
	props.Duplicate = DuplicateInvariant
	if props.Merge == "SUM" {
		props.Rollup = RollupHolistic
		props.PartialState = nil
		props.Merge = ""
		props.Finalize = FinalizeNone
	}
	return props
}

// hasDistinctArgument reports whether any argument is DISTINCT-qualified.
//
// Distinctness is a property of the argument rather than of the function.
// expression/parser.go parses DISTINCT as a unary operator, so COUNT(DISTINCT x)
// is COUNT applied to UnaryExpr{Op: "DISTINCT"}. A property table keyed on
// function name alone cannot represent it.
func hasDistinctArgument(call *FunctionCallExpr) bool {
	for _, arg := range call.Args {
		if unary, ok := arg.(*UnaryExpr); ok && strings.EqualFold(unary.Op, "DISTINCT") {
			return true
		}
	}
	return false
}

func anyAggregateSignature(signatures []FunctionSignature) bool {
	for _, signature := range signatures {
		if signature.Kind == FunctionAggregate {
			return true
		}
	}
	return false
}

// CollectAggregationProperties derives properties for every aggregation call
// site in an expression, in deterministic source order.
func CollectAggregationProperties(expr Expr, registry *FunctionRegistry) []AggregationProperties {
	if expr == nil {
		return nil
	}
	collector := &aggregationPropertyCollector{registry: registry}
	Walk(expr, collector)
	return collector.properties
}

type aggregationPropertyCollector struct {
	registry   *FunctionRegistry
	properties []AggregationProperties
}

func (c *aggregationPropertyCollector) Enter(expr Expr) bool {
	call, ok := expr.(*FunctionCallExpr)
	if !ok {
		return true
	}
	if props, carries := DeriveAggregationProperties(c.registry, call); carries {
		c.properties = append(c.properties, props)
	}
	return true
}

func (c *aggregationPropertyCollector) Leave(Expr) {}
