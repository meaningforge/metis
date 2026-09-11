package expression

import "testing"

func TestResolveFunctionSignaturesPrefersExactMatch(t *testing.T) {
	integer := FunctionSignature{
		Name: "score", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeInteger},
		ReturnType: func([]SemanticType) SemanticType { return TypeInteger }, Deterministic: true,
	}
	decimal := FunctionSignature{
		Name: "score", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeDecimal},
		ReturnType: func([]SemanticType) SemanticType { return TypeDecimal }, Deterministic: true,
	}

	got, err := ResolveFunctionSignatures("score", []FunctionSignature{decimal, integer}, []SemanticType{TypeInteger})
	if err != nil {
		t.Fatal(err)
	}
	if got.Signature.ReturnType(got.ParameterTypes) != TypeInteger {
		t.Fatalf("resolved return type=%s", got.Signature.ReturnType(got.ParameterTypes))
	}
	if got.Cost != 0 || len(got.Coercions) != 0 {
		t.Fatalf("cost=%d coercions=%#v", got.Cost, got.Coercions)
	}
}

func TestResolveFunctionSignaturesRecordsIntegerToDecimalCoercion(t *testing.T) {
	decimal := FunctionSignature{
		Name: "decimal_only", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeDecimal},
		ReturnType: func([]SemanticType) SemanticType { return TypeDecimal }, Deterministic: true,
	}

	got, err := ResolveFunctionSignatures("decimal_only", []FunctionSignature{decimal}, []SemanticType{TypeInteger})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cost != 1 || len(got.Coercions) != 1 {
		t.Fatalf("cost=%d coercions=%#v", got.Cost, got.Coercions)
	}
	coercion := got.Coercions[0]
	if coercion.Index != 0 || coercion.From != TypeInteger || coercion.To != TypeDecimal {
		t.Fatalf("coercion=%#v", coercion)
	}
	if got.ParameterTypes[0] != TypeDecimal {
		t.Fatalf("parameter type=%s", got.ParameterTypes[0])
	}
}

func TestResolveFunctionSignaturesRejectsEqualCostAmbiguity(t *testing.T) {
	integer := FunctionSignature{
		Name: "choose", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{AnyType}, ArgTargets: []SemanticType{TypeInteger},
		ReturnType: func([]SemanticType) SemanticType { return TypeInteger }, Deterministic: true,
	}
	decimal := FunctionSignature{
		Name: "choose", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{AnyType}, ArgTargets: []SemanticType{TypeDecimal},
		ReturnType: func([]SemanticType) SemanticType { return TypeDecimal }, Deterministic: true,
	}

	_, err := ResolveFunctionSignatures("choose", []FunctionSignature{integer, decimal}, []SemanticType{TypeUnknown})
	re, ok := err.(*FunctionResolutionError)
	if !ok || re.Failure != FunctionResolutionAmbiguous {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestResolveFunctionSignaturesDoesNotImplicitlyNarrowDecimal(t *testing.T) {
	integer := FunctionSignature{
		Name: "integer_only", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeInteger},
		ReturnType: func([]SemanticType) SemanticType { return TypeInteger }, Deterministic: true,
	}

	_, err := ResolveFunctionSignatures("integer_only", []FunctionSignature{integer}, []SemanticType{TypeDecimal})
	re, ok := err.(*FunctionResolutionError)
	if !ok || re.Failure != FunctionResolutionNoMatch {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestFunctionRegistryPreservesOverloadSet(t *testing.T) {
	integer := FunctionSignature{Name: "f", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1, ArgTargets: []SemanticType{TypeInteger}}
	decimal := FunctionSignature{Name: "F", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1, ArgTargets: []SemanticType{TypeDecimal}}
	registry := NewFunctionRegistry(integer, decimal)

	got := registry.Signatures("f")
	if len(got) != 2 {
		t.Fatalf("signatures=%#v", got)
	}
	got[0].Name = "mutated"
	again := registry.Signatures("f")
	if again[0].Name != "f" {
		t.Fatalf("registry leaked mutable signature slice: %#v", again)
	}
}
