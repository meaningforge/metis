package expression

import "testing"

func TestSemanticTypeConstraintsAcceptOnlyCompatibleTypes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		constraint TypeConstraint
		accepted   []SemanticType
		rejected   SemanticType
	}{
		{"numeric", NumericType, []SemanticType{TypeUnknown, TypeNull, TypeInteger, TypeDecimal}, TypeString},
		{"string", StringType, []SemanticType{TypeUnknown, TypeNull, TypeString}, TypeBoolean},
		{"boolean", BooleanType, []SemanticType{TypeUnknown, TypeNull, TypeBoolean}, TypeDecimal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, typ := range tc.accepted {
				if !tc.constraint(typ) {
					t.Fatalf("constraint(%s) = false, want true", typ)
				}
			}
			if tc.constraint(tc.rejected) {
				t.Fatalf("constraint(%s) = true, want false", tc.rejected)
			}
		})
	}
	if !AnyType(TypeArray) {
		t.Fatal("AnyType(ARRAY) = false, want true")
	}
}

func TestFunctionSignatureAcceptsFixedAndVariadicArguments(t *testing.T) {
	sig := FunctionSignature{
		ArgTypes:     []TypeConstraint{StringType},
		VariadicType: NumericType,
	}
	if !sig.AcceptsArg(0, TypeString) || sig.AcceptsArg(0, TypeInteger) {
		t.Fatal("fixed argument constraint was not applied")
	}
	if !sig.AcceptsArg(1, TypeDecimal) || sig.AcceptsArg(1, TypeBoolean) {
		t.Fatal("variadic argument constraint was not applied")
	}
}

func TestFunctionSignatureTargetTypeUsesFixedThenVariadicTarget(t *testing.T) {
	sig := FunctionSignature{
		ArgTargets:     []SemanticType{TypeString, TypeDecimal},
		VariadicTarget: TypeInteger,
	}
	for index, want := range []SemanticType{TypeString, TypeDecimal, TypeInteger} {
		if got := sig.TargetType(index); got != want {
			t.Fatalf("TargetType(%d) = %s, want %s", index, got, want)
		}
	}
}

func TestFunctionRegistryLookupIsCaseInsensitiveAndStable(t *testing.T) {
	first := FunctionSignature{Name: "demo", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1}
	second := FunctionSignature{Name: "DEMO", Kind: FunctionAggregate, MinArgs: 2, MaxArgs: 2}
	registry := NewFunctionRegistry(first, second)

	got, ok := registry.Lookup("DeMo")
	if !ok || got.Kind != FunctionScalar {
		t.Fatalf("Lookup() = %#v, %v; want first registered signature", got, ok)
	}
	if _, ok := (*FunctionRegistry)(nil).Lookup("demo"); ok {
		t.Fatal("nil registry Lookup() succeeded")
	}
}

func TestFunctionRegistrySignaturesReturnsOwnedSlice(t *testing.T) {
	registry := NewFunctionRegistry(
		FunctionSignature{Name: "demo", Kind: FunctionScalar},
		FunctionSignature{Name: "demo", Kind: FunctionAggregate},
	)
	sigs := registry.Signatures("DEMO")
	if len(sigs) != 2 {
		t.Fatalf("Signatures() len = %d, want 2", len(sigs))
	}
	sigs[0].Name = "mutated"

	again := registry.Signatures("demo")
	if again[0].Name != "demo" {
		t.Fatalf("registry signature mutated through returned slice: %#v", again[0])
	}
	if got := (*FunctionRegistry)(nil).Signatures("demo"); got != nil {
		t.Fatalf("nil registry Signatures() = %#v, want nil", got)
	}
}
