package expression

import (
	"testing"

	"github.com/meaningforge/metis/extension"
)

// The planner's evaluation ownership gate cannot see inside an
// ExtensionEvidenceSet -- values is unexported, so reflection can neither fill
// it nor compare it. What that gate can observe is whether two sets are the
// same object, and it treats a distinct set as proof of isolated storage.
//
// That inference is only sound because of what this test proves: a set never
// shares a backing array with anything outside this package. Constructing one
// copies the caller's slice and reading one hands back a copy, so there is no
// route from a set to the storage of another set or of its caller. If either
// copy were dropped, two distinct sets could still alias, and the planner gate
// would report ownership it had not established.
func TestExtensionEvidenceSetOwnsItsStorage(t *testing.T) {
	first := extension.MetricScaleEvidence{Metric: "revenue", Version: "v1", Factor: 2}
	second := extension.MetricScaleEvidence{Metric: "cost", Version: "v1", Factor: 3}

	input := []extension.Evidence{first, second}
	resolved := NewResolvedExpression("ANSI_SQL", "SUM(amount)").WithExtensionEvidence(input...)

	input[0] = second
	if got := resolved.ExtensionEvidence.values[0]; got != extension.Evidence(first) {
		t.Fatalf("mutating the constructor's input reached the set: got %v, want %v", got, first)
	}

	read := resolved.ExtensionEvidenceValues()
	read[0] = second
	if got := resolved.ExtensionEvidence.values[0]; got != extension.Evidence(first) {
		t.Fatalf("mutating a read-back slice reached the set: got %v, want %v", got, first)
	}

	// The round-trip the planner's evaluation clone performs. A new set every
	// time, so pointer inequality is a reliable signal there.
	clone := resolved.WithExtensionEvidence(resolved.ExtensionEvidenceValues()...)
	if clone.ExtensionEvidence == resolved.ExtensionEvidence {
		t.Fatal("round-tripping evidence through the accessor reused the set")
	}
	if &clone.ExtensionEvidence.values[0] == &resolved.ExtensionEvidence.values[0] {
		t.Fatal("round-tripping evidence through the accessor shared the backing array")
	}
	if len(clone.ExtensionEvidence.values) != 2 || clone.ExtensionEvidence.values[1] != extension.Evidence(second) {
		t.Fatalf("round-trip did not carry the evidence: %v", clone.ExtensionEvidence.values)
	}
}

func TestResolvedExpressionRequiresDialectAndSource(t *testing.T) {
	for _, test := range []struct {
		name string
		expr ResolvedExpression
		want bool
	}{
		{name: "resolved", expr: NewResolvedExpression("ANSI_SQL", "amount"), want: true},
		{name: "missing dialect", expr: NewResolvedExpression("", "amount")},
		{name: "missing source", expr: NewResolvedExpression("ANSI_SQL", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.expr.IsResolved(); got != test.want {
				t.Fatalf("IsResolved() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestResolvedExpressionOwnsSelectedAggregationProperties(t *testing.T) {
	properties := []AggregationProperties{{
		Function: "COUNT", Duplicate: DuplicateInvariant,
		PartialState: []PartialStateComponent{{Name: PartialStateCount}},
	}}
	resolved := NewResolvedExpression("ANSI_SQL", "COUNT(DISTINCT customer_id)").
		WithAnalysis(BoundExpression{}, TypedExpression{}).
		WithAggregationProperties(properties...)
	properties[0].Duplicate = DuplicateSensitive
	properties[0].PartialState[0].Name = "mutated"

	got := resolved.AggregationPropertyValues()
	if len(got) != 1 || got[0].Duplicate != DuplicateInvariant || got[0].PartialState[0].Name != PartialStateCount {
		t.Fatalf("aggregation properties = %#v", got)
	}
	got[0].PartialState[0].Name = "mutated again"
	if again := resolved.AggregationPropertyValues()[0].PartialState[0].Name; again != PartialStateCount {
		t.Fatalf("aggregation property accessor aliases analysis: %q", again)
	}
}
