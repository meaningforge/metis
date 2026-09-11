package expression

import "testing"

type legacyAnalyzerResolver map[string]BoundSymbol

func (r legacyAnalyzerResolver) Resolve(parts []string) (BoundSymbol, bool) {
	ref := referenceFromParts(parts)
	symbol, ok := r[referenceName(ref)]
	return symbol, ok
}

func TestResolveAnalyzerSymbolPreservesEvidenceAwareScope(t *testing.T) {
	scope := NewSymbolScope(ScopeMetric, nil, BoundSymbol{
		Kind: BoundMetric, Name: "revenue", Type: TypeDecimal, Aggregation: AggregationAggregate,
	})
	resolver := NewPrecedenceResolver(scope, nil)
	span := SourceSpan{Start: 3, End: 10}

	symbol, evidence, err := resolveAnalyzerSymbol(resolver, []string{"revenue"}, span)
	if err != nil {
		t.Fatal(err)
	}
	if symbol.Kind != BoundMetric || evidence.SelectedScope != ScopeMetric || evidence.Span != span {
		t.Fatalf("symbol=%#v evidence=%#v", symbol, evidence)
	}
}

func TestResolveAnalyzerSymbolWrapsLegacyResolverAsExternalEvidence(t *testing.T) {
	resolver := legacyAnalyzerResolver{
		"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
	}
	span := SourceSpan{Start: 1, End: 14}

	symbol, evidence, err := resolveAnalyzerSymbol(resolver, []string{"orders", "amount"}, span)
	if err != nil {
		t.Fatal(err)
	}
	if symbol.Kind != BoundColumn || evidence.SelectedScope != ScopeExternal || evidence.Span != span {
		t.Fatalf("symbol=%#v evidence=%#v", symbol, evidence)
	}
}

func TestSemanticBindingErrorMapsAmbiguityWithSourceSpan(t *testing.T) {
	scope := NewSymbolScope(ScopeQuery, nil,
		BoundSymbol{Kind: BoundColumn, Qualifier: "orders", Name: "id"},
		BoundSymbol{Kind: BoundColumn, Qualifier: "customers", Name: "id"},
	)
	resolver := NewPrecedenceResolver(scope, nil)
	span := SourceSpan{Start: 5, End: 7}

	_, evidence, err := resolveAnalyzerSymbol(resolver, []string{"id"}, span)
	mapped := semanticBindingError(err, evidence)
	semanticErr, ok := mapped.(*SemanticError)
	if !ok || semanticErr.Code != ErrAmbiguousSymbol || semanticErr.Span != span {
		t.Fatalf("err=%T %#v", mapped, mapped)
	}
}

func TestSemanticBindingErrorMapsUnboundWithSourceSpan(t *testing.T) {
	span := SourceSpan{Start: 8, End: 15}
	_, evidence, err := resolveAnalyzerSymbol(legacyAnalyzerResolver{}, []string{"missing"}, span)
	mapped := semanticBindingError(err, evidence)
	semanticErr, ok := mapped.(*SemanticError)
	if !ok || semanticErr.Code != ErrUnboundSymbol || semanticErr.Span != span {
		t.Fatalf("err=%T %#v", mapped, mapped)
	}
}
