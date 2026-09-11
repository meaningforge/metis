package expression

import "testing"

func TestResolveExplicitCastAllowsAuthoredNarrowingWithoutChangingImplicitCoercion(t *testing.T) {
	got, err := ResolveExplicitCast(TypeDecimal, TypeInteger, NullabilityNonNull)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ExplicitCastNumeric || got.To != TypeInteger {
		t.Fatalf("resolution=%#v", got)
	}
	if got.Nullability != NullabilityNonNull {
		t.Fatalf("nullability=%s", got.Nullability)
	}
}

func TestResolveExplicitCastSupportsStringAndSemiStructuredScalarCasts(t *testing.T) {
	tests := []struct {
		name string
		from SemanticType
		to   SemanticType
		kind ExplicitCastKind
	}{
		{name: "string to decimal", from: TypeString, to: TypeDecimal, kind: ExplicitCastTextual},
		{name: "variant to string", from: TypeVariant, to: TypeString, kind: ExplicitCastTextual},
		{name: "variant to timestamp", from: TypeVariant, to: TypeTimestamp, kind: ExplicitCastSemiStructured},
		{name: "date to timestamp", from: TypeDate, to: TypeTimestamp, kind: ExplicitCastTemporal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveExplicitCast(tt.from, tt.to, NullabilityUnknown)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.kind {
				t.Fatalf("kind=%s", got.Kind)
			}
		})
	}
}

func TestResolveExplicitCastPreservesNullabilityAndNullLiteralIsNullable(t *testing.T) {
	got, err := ResolveExplicitCast(TypeString, TypeDecimal, NullabilityNullable)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nullability != NullabilityNullable {
		t.Fatalf("nullability=%s", got.Nullability)
	}

	nullCast, err := ResolveExplicitCast(TypeNull, TypeString, NullabilityNonNull)
	if err != nil {
		t.Fatal(err)
	}
	if nullCast.Kind != ExplicitCastNull || nullCast.Nullability != NullabilityNullable {
		t.Fatalf("resolution=%#v", nullCast)
	}
}

func TestResolveExplicitCastRejectsUnknownTargetsAndCrossContainerCasts(t *testing.T) {
	_, err := ResolveExplicitCast(TypeString, TypeUnknown, NullabilityUnknown)
	castErr, ok := err.(*CastResolutionError)
	if !ok || castErr.Failure != CastResolutionInvalidTarget {
		t.Fatalf("err=%T %v", err, err)
	}

	_, err = ResolveExplicitCast(TypeArray, TypeDecimal, NullabilityUnknown)
	castErr, ok = err.(*CastResolutionError)
	if !ok || castErr.Failure != CastResolutionUnsupported {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestMergeNullabilityIsFailClosed(t *testing.T) {
	if got := MergeNullability(NullabilityNonNull, NullabilityNonNull); got != NullabilityNonNull {
		t.Fatalf("got=%s", got)
	}
	if got := MergeNullability(NullabilityNonNull, NullabilityUnknown); got != NullabilityUnknown {
		t.Fatalf("got=%s", got)
	}
	if got := MergeNullability(NullabilityUnknown, NullabilityNullable); got != NullabilityNullable {
		t.Fatalf("got=%s", got)
	}
}
