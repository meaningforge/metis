package expression

import "testing"

type scopeAwareTestResolver struct {
	symbols  []BoundSymbol
	fallback map[string]BoundSymbol
	calls    int
}

func (r *scopeAwareTestResolver) Resolve(parts []string) (BoundSymbol, bool) {
	r.calls++
	ref := referenceFromParts(parts)
	symbol, ok := r.fallback[referenceName(ref)]
	return symbol, ok
}

func (r *scopeAwareTestResolver) Symbols() []BoundSymbol {
	return append([]BoundSymbol(nil), r.symbols...)
}

func TestSemanticAnalyzerMetricScopePrecedesQueryAndExternal(t *testing.T) {
	ast, err := Parse("revenue", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &scopeAwareTestResolver{
		symbols: []BoundSymbol{
			{Kind: BoundColumn, Qualifier: "orders", Name: "revenue", Type: TypeDecimal},
			{Kind: BoundMetric, Name: "revenue", Type: TypeDecimal, Aggregation: AggregationAggregate},
		},
		fallback: map[string]BoundSymbol{
			"revenue": {Kind: BoundColumn, Qualifier: "legacy", Name: "revenue", Type: TypeDecimal},
		},
	}

	got, err := NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Symbols) != 1 || got.Symbols[0].Kind != BoundMetric {
		t.Fatalf("symbols=%#v", got.Symbols)
	}
	if len(got.BindingResolutions) != 1 || got.BindingResolutions[0].SelectedScope != ScopeMetric {
		t.Fatalf("binding evidence=%#v", got.BindingResolutions)
	}
	if resolver.calls != 0 {
		t.Fatalf("external fallback calls=%d", resolver.calls)
	}
}

func TestSemanticAnalyzerLambdaScopeShadowsQuerySymbol(t *testing.T) {
	ast, err := Parse("arrayMap(amount -> amount + orders.tax, orders.values)", ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(FunctionSignature{
		Name: "arrayMap", Kind: FunctionScalar, MinArgs: 2, MaxArgs: 2,
		ArgTypes: []TypeConstraint{AnyType, AnyType}, ReturnType: func([]SemanticType) SemanticType { return TypeArray }, Deterministic: true,
	})
	resolver := &scopeAwareTestResolver{
		symbols: []BoundSymbol{
			{Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
			{Kind: BoundColumn, Qualifier: "orders", Name: "tax", Type: TypeDecimal},
			{Kind: BoundColumn, Qualifier: "orders", Name: "values", Type: TypeArray},
		},
		fallback: map[string]BoundSymbol{},
	}

	got, err := NewSemanticAnalyzer(resolver, registry).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Symbols) != 2 {
		t.Fatalf("external symbols=%#v", got.Symbols)
	}
	var localSeen, taxSeen, valuesSeen bool
	for _, evidence := range got.BindingResolutions {
		switch {
		case evidence.Reference.Name == "amount" && evidence.SelectedScope == ScopeLambda:
			localSeen = true
		case evidence.Reference.Qualifier == "orders" && evidence.Reference.Name == "tax" && evidence.SelectedScope == ScopeQuery:
			taxSeen = true
		case evidence.Reference.Qualifier == "orders" && evidence.Reference.Name == "values" && evidence.SelectedScope == ScopeQuery:
			valuesSeen = true
		}
	}
	if !localSeen || !taxSeen || !valuesSeen {
		t.Fatalf("binding evidence=%#v", got.BindingResolutions)
	}
	for _, symbol := range got.Symbols {
		if symbol.Kind == BoundLocal {
			t.Fatalf("lambda local leaked into external symbols: %#v", got.Symbols)
		}
	}
}

func TestSemanticAnalyzerNestedLambdaCapturesOuterLocal(t *testing.T) {
	ast, err := Parse("arrayMap(x -> arrayMap(y -> x + y, orders.values), orders.values)", ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(FunctionSignature{
		Name: "arrayMap", Kind: FunctionScalar, MinArgs: 2, MaxArgs: 2,
		ArgTypes: []TypeConstraint{AnyType, AnyType}, ReturnType: func([]SemanticType) SemanticType { return TypeArray }, Deterministic: true,
	})
	resolver := &scopeAwareTestResolver{
		symbols:  []BoundSymbol{{Kind: BoundColumn, Qualifier: "orders", Name: "values", Type: TypeArray}},
		fallback: map[string]BoundSymbol{},
	}

	got, err := NewSemanticAnalyzer(resolver, registry).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	locals := map[string]int{}
	for _, evidence := range got.BindingResolutions {
		if evidence.SelectedScope == ScopeLambda {
			locals[evidence.Reference.Name]++
		}
	}
	if locals["x"] == 0 || locals["y"] == 0 {
		t.Fatalf("binding evidence=%#v", got.BindingResolutions)
	}
	for _, symbol := range got.Symbols {
		if symbol.Kind == BoundLocal {
			t.Fatalf("nested lambda local leaked into external symbols: %#v", got.Symbols)
		}
	}
}

func TestSemanticAnalyzerQueryAmbiguityFailsBeforeExternalFallback(t *testing.T) {
	ast, err := Parse("id", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &scopeAwareTestResolver{
		symbols: []BoundSymbol{
			{Kind: BoundColumn, Qualifier: "orders", Name: "id", Type: TypeInteger},
			{Kind: BoundColumn, Qualifier: "customers", Name: "id", Type: TypeInteger},
		},
		fallback: map[string]BoundSymbol{
			"id": {Kind: BoundColumn, Qualifier: "legacy", Name: "id", Type: TypeInteger},
		},
	}

	_, err = NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	semanticErr, ok := err.(*SemanticError)
	if !ok || semanticErr.Code != ErrAmbiguousSymbol {
		t.Fatalf("err=%T %v", err, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("external fallback calls=%d", resolver.calls)
	}
}
