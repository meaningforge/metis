package expression

import (
	"strings"

	"github.com/meaningforge/metis/extension"
)

// ResolvedExpression is the stable value passed across the semantic
// resolution and planning boundary. SourceDialect records which declared
// implementation won target-specific -> ANSI fallback selection. Analysis is
// present when SemanticManifest has already bound and typed the selected expression.
type ResolvedExpression struct {
	SourceDialect     string
	Source            string
	Analysis          *ResolvedAnalysis
	ExtensionEvidence *ExtensionEvidenceSet
}

type ResolvedAnalysis struct {
	Bound                 BoundExpression
	Typed                 TypedExpression
	aggregationProperties []AggregationProperties
}

// ExtensionEvidenceSet keeps typed semantic-critical evidence attached to a
// resolved expression without making ResolvedExpression non-comparable.
//
// The set is immutable from outside this package: values is unexported,
// WithExtensionEvidence copies the slice it is given, and
// ExtensionEvidenceValues returns a copy. Nothing can reach the backing array
// of a set it did not construct, which is what lets a set be handed across the
// resolution and planning boundary safely.
//
// This is deliberately NOT the Analysis contract, and this comment used to say
// it was. Analysis pointer identity survives a copy through planner structures;
// evidence does not, because planner's evaluation clone rebuilds the set from
// the accessor. The implementation was always the stricter of the two -- a
// cloned evaluation owns its evidence outright -- and the two statements
// contradicted each other until Phase K's ownership gate made the clone's
// behaviour observable. The implementation is the contract; this now says so.
type ExtensionEvidenceSet struct {
	values []extension.Evidence
}

func NewResolvedExpression(sourceDialect, source string) ResolvedExpression {
	return ResolvedExpression{SourceDialect: sourceDialect, Source: source}
}

func (e ResolvedExpression) WithAnalysis(bound BoundExpression, typed TypedExpression) ResolvedExpression {
	e.Analysis = &ResolvedAnalysis{Bound: bound, Typed: typed}
	return e
}

// WithAggregationProperties attaches the algebra derived for the selected
// expression implementation. The slice is owned here so Resolver can hand the
// analysis across the planning boundary without exposing SemanticManifest storage.
func (e ResolvedExpression) WithAggregationProperties(properties ...AggregationProperties) ResolvedExpression {
	if e.Analysis == nil {
		return e
	}
	analysis := *e.Analysis
	analysis.aggregationProperties = append([]AggregationProperties(nil), properties...)
	for i := range analysis.aggregationProperties {
		analysis.aggregationProperties[i].PartialState = append([]PartialStateComponent(nil), properties[i].PartialState...)
	}
	e.Analysis = &analysis
	return e
}

func (e ResolvedExpression) AggregationPropertyValues() []AggregationProperties {
	if e.Analysis == nil {
		return nil
	}
	out := append([]AggregationProperties(nil), e.Analysis.aggregationProperties...)
	for i := range out {
		out[i].PartialState = append([]PartialStateComponent(nil), e.Analysis.aggregationProperties[i].PartialState...)
	}
	return out
}

func (e ResolvedExpression) WithExtensionEvidence(evidence ...extension.Evidence) ResolvedExpression {
	if len(evidence) == 0 {
		e.ExtensionEvidence = nil
		return e
	}
	e.ExtensionEvidence = &ExtensionEvidenceSet{values: append([]extension.Evidence(nil), evidence...)}
	return e
}

func (e ResolvedExpression) ExtensionEvidenceValues() []extension.Evidence {
	if e.ExtensionEvidence == nil {
		return nil
	}
	return append([]extension.Evidence(nil), e.ExtensionEvidence.values...)
}

func (e ResolvedExpression) IsResolved() bool {
	return strings.TrimSpace(e.SourceDialect) != "" && strings.TrimSpace(e.Source) != ""
}

func (e ResolvedExpression) SameSource(other ResolvedExpression) bool {
	return e.SourceDialect == other.SourceDialect && e.Source == other.Source
}
