package expression

import "testing"

type countingSymbolResolver struct {
	symbols map[string]BoundSymbol
	calls   int
}

func (r *countingSymbolResolver) Resolve(parts []string) (BoundSymbol, bool) {
	r.calls++
	key := ""
	for i, part := range parts {
		if i > 0 {
			key += "."
		}
		key += part
	}
	symbol, ok := r.symbols[key]
	return symbol, ok
}

func TestPrecedenceResolverExplicitScopeWinsWithoutFallback(t *testing.T) {
	fallback := &countingSymbolResolver{symbols: map[string]BoundSymbol{
		"amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
	}}
	scope := NewSymbolScope(ScopeMetric, nil, BoundSymbol{
		Kind: BoundMetric, Name: "amount", Type: TypeDecimal, Aggregation: AggregationAggregate,
	})
	resolver := NewPrecedenceResolver(scope, fallback)

	got, evidence, err := resolver.ResolveWithEvidence([]string{"amount"}, SourceSpan{Start: 1, End: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != BoundMetric || got.Name != "amount" {
		t.Fatalf("selected=%#v", got)
	}
	if evidence.SelectedScope != ScopeMetric || evidence.Reason != BindingReasonNearestScope {
		t.Fatalf("evidence=%#v", evidence)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback calls=%d", fallback.calls)
	}
}

func TestPrecedenceResolverFallsBackOnlyWhenExplicitScopesDoNotMatch(t *testing.T) {
	fallback := &countingSymbolResolver{symbols: map[string]BoundSymbol{
		"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
	}}
	scope := NewSymbolScope(ScopeLambda, nil, BoundSymbol{Kind: BoundLocal, Name: "amount", Type: TypeUnknown})
	resolver := NewPrecedenceResolver(scope, fallback)

	got, evidence, err := resolver.ResolveWithEvidence([]string{"orders", "amount"}, SourceSpan{Start: 3, End: 16})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != BoundColumn || got.Qualifier != "orders" {
		t.Fatalf("selected=%#v", got)
	}
	if evidence.SelectedScope != ScopeExternal || len(evidence.Candidates) != 1 {
		t.Fatalf("evidence=%#v", evidence)
	}
	if evidence.Span.Start != 3 || evidence.Span.End != 16 {
		t.Fatalf("span=%#v", evidence.Span)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback calls=%d", fallback.calls)
	}
}

func TestPrecedenceResolverAmbiguityFailsClosedWithoutFallback(t *testing.T) {
	fallback := &countingSymbolResolver{symbols: map[string]BoundSymbol{
		"id": {Kind: BoundColumn, Qualifier: "legacy", Name: "id", Type: TypeInteger},
	}}
	scope := NewSymbolScope(ScopeQuery, nil,
		BoundSymbol{Kind: BoundColumn, Qualifier: "orders", Name: "id", Type: TypeInteger},
		BoundSymbol{Kind: BoundColumn, Qualifier: "customers", Name: "id", Type: TypeInteger},
	)
	resolver := NewPrecedenceResolver(scope, fallback)

	_, evidence, err := resolver.ResolveWithEvidence([]string{"id"}, SourceSpan{Start: 5, End: 7})
	bindingErr, ok := err.(*ScopeBindingError)
	if !ok || bindingErr.Code != ScopeBindingAmbiguous {
		t.Fatalf("err=%T %v", err, err)
	}
	if evidence.Reason != BindingReasonAmbiguous || len(evidence.Candidates) != 2 {
		t.Fatalf("evidence=%#v", evidence)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback calls=%d", fallback.calls)
	}
}

func TestPrecedenceResolverUnboundRetainsSpanAfterFallbackMiss(t *testing.T) {
	fallback := &countingSymbolResolver{symbols: map[string]BoundSymbol{}}
	resolver := NewPrecedenceResolver(nil, fallback)
	span := SourceSpan{Start: 8, End: 15}

	_, evidence, err := resolver.ResolveWithEvidence([]string{"missing"}, span)
	bindingErr, ok := err.(*ScopeBindingError)
	if !ok || bindingErr.Code != ScopeBindingUnbound {
		t.Fatalf("err=%T %v", err, err)
	}
	if evidence.Reference.Name != "missing" || evidence.Span != span || evidence.Reason != BindingReasonUnbound {
		t.Fatalf("evidence=%#v", evidence)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback calls=%d", fallback.calls)
	}
}

func TestPrecedenceResolverCompatibilityResolveRejectsAmbiguity(t *testing.T) {
	scope := NewSymbolScope(ScopeQuery, nil,
		BoundSymbol{Kind: BoundColumn, Qualifier: "orders", Name: "id"},
		BoundSymbol{Kind: BoundColumn, Qualifier: "customers", Name: "id"},
	)
	resolver := NewPrecedenceResolver(scope, nil)

	if _, ok := resolver.Resolve([]string{"id"}); ok {
		t.Fatal("ambiguous reference must not resolve through compatibility API")
	}
}
