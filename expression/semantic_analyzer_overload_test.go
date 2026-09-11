package expression

import "testing"

func TestSemanticAnalyzerSelectsExactOverCoercedOverload(t *testing.T) {
	ast, err := Parse("pick(orders.count)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(
		FunctionSignature{
			Name: "pick", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
			ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeDecimal},
			ReturnType: func([]SemanticType) SemanticType { return TypeDecimal }, Deterministic: true,
		},
		FunctionSignature{
			Name: "pick", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
			ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeInteger},
			ReturnType: func([]SemanticType) SemanticType { return TypeInteger }, Deterministic: true,
		},
	)
	resolver := testResolver{
		"orders.count": {Kind: BoundColumn, Qualifier: "orders", Name: "count", Type: TypeInteger},
	}

	got, err := NewSemanticAnalyzer(resolver, registry).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeInteger {
		t.Fatalf("type=%s", got.Type)
	}
	if len(got.FunctionResolutions) != 1 {
		t.Fatalf("function resolutions=%#v", got.FunctionResolutions)
	}
	evidence := got.FunctionResolutions[0]
	if evidence.Cost != 0 || len(evidence.Coercions) != 0 {
		t.Fatalf("evidence=%#v", evidence)
	}
	if len(evidence.ParameterTypes) != 1 || evidence.ParameterTypes[0] != TypeInteger {
		t.Fatalf("parameter types=%#v", evidence.ParameterTypes)
	}
}

func TestSemanticAnalyzerRetainsFunctionCoercionEvidence(t *testing.T) {
	ast, err := Parse("to_decimal(orders.count)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(FunctionSignature{
		Name: "to_decimal", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeDecimal},
		ReturnType: func(args []SemanticType) SemanticType { return args[0] }, Deterministic: true,
	})
	resolver := testResolver{
		"orders.count": {Kind: BoundColumn, Qualifier: "orders", Name: "count", Type: TypeInteger},
	}

	got, err := NewSemanticAnalyzer(resolver, registry).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
	if len(got.FunctionResolutions) != 1 {
		t.Fatalf("function resolutions=%#v", got.FunctionResolutions)
	}
	evidence := got.FunctionResolutions[0]
	if evidence.Cost != 1 || len(evidence.Coercions) != 1 {
		t.Fatalf("evidence=%#v", evidence)
	}
	coercion := evidence.Coercions[0]
	if coercion.Index != 0 || coercion.From != TypeInteger || coercion.To != TypeDecimal {
		t.Fatalf("coercion=%#v", coercion)
	}
}

func TestSemanticAnalyzerRejectsAmbiguousOverloadWithSourceSpan(t *testing.T) {
	ast, err := Parse("pick(NULL)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(
		FunctionSignature{
			Name: "pick", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
			ArgTypes: []TypeConstraint{AnyType}, ArgTargets: []SemanticType{TypeInteger},
			ReturnType: func([]SemanticType) SemanticType { return TypeInteger }, Deterministic: true,
		},
		FunctionSignature{
			Name: "pick", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
			ArgTypes: []TypeConstraint{AnyType}, ArgTargets: []SemanticType{TypeString},
			ReturnType: func([]SemanticType) SemanticType { return TypeString }, Deterministic: true,
		},
	)

	_, err = NewSemanticAnalyzer(nil, registry).Analyze(ast)
	semanticErr, ok := err.(*SemanticError)
	if !ok || semanticErr.Code != ErrAmbiguousFunction {
		t.Fatalf("err=%T %v", err, err)
	}
	if semanticErr.Span != SpanOf(ast) {
		t.Fatalf("span=%#v want=%#v", semanticErr.Span, SpanOf(ast))
	}
}

func TestSemanticAnalyzerRejectsNoMatchingOverloadWithSourceSpan(t *testing.T) {
	ast, err := Parse("numeric_only('paid')", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(FunctionSignature{
		Name: "numeric_only", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1,
		ArgTypes: []TypeConstraint{NumericType}, ArgTargets: []SemanticType{TypeDecimal},
		ReturnType: func([]SemanticType) SemanticType { return TypeDecimal }, Deterministic: true,
	})

	_, err = NewSemanticAnalyzer(nil, registry).Analyze(ast)
	semanticErr, ok := err.(*SemanticError)
	if !ok || semanticErr.Code != ErrTypeMismatch {
		t.Fatalf("err=%T %v", err, err)
	}
	if semanticErr.Span != SpanOf(ast) {
		t.Fatalf("span=%#v want=%#v", semanticErr.Span, SpanOf(ast))
	}
}
