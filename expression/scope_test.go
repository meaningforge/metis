package expression

import "testing"

func TestScopeBindingErrorFormatsReferenceCodeAndSpan(t *testing.T) {
	err := (&ScopeBindingError{
		Code: ScopeBindingAmbiguous,
		Evidence: SymbolBindingEvidence{
			Reference: Reference{Qualifier: "orders", Name: "id"},
			Span:      SourceSpan{Start: 3, End: 12},
		},
	}).Error()
	want := "scope binding AMBIGUOUS for orders.id at [3,12)"
	if err != want {
		t.Fatalf("Error() = %q, want %q", err, want)
	}
}

func TestScopeBindingErrorHandlesNilReceiver(t *testing.T) {
	var err *ScopeBindingError
	if got := err.Error(); got != "" {
		t.Fatalf("Error() = %q, want empty string", got)
	}
}

func TestSymbolScopeNearestScopeShadowsOuter(t *testing.T) {
	query := NewSymbolScope(ScopeQuery, nil, BoundSymbol{
		Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal,
	})
	lambda := NewSymbolScope(ScopeLambda, query, BoundSymbol{
		Kind: BoundLocal, Name: "amount", Type: TypeUnknown,
	})

	got, evidence, err := lambda.Resolve(Reference{Name: "amount"}, SourceSpan{Start: 4, End: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != BoundLocal || got.Name != "amount" {
		t.Fatalf("selected=%#v", got)
	}
	if !evidence.Found || evidence.SelectedScope != ScopeLambda || evidence.Reason != BindingReasonNearestScope {
		t.Fatalf("evidence=%#v", evidence)
	}
	if len(evidence.Candidates) != 1 || evidence.Candidates[0].Scope != ScopeLambda {
		t.Fatalf("candidates=%#v", evidence.Candidates)
	}
	if evidence.Span.Start != 4 || evidence.Span.End != 10 {
		t.Fatalf("span=%#v", evidence.Span)
	}
}

func TestSymbolScopeQualifiedReferenceSkipsNonMatchingInnerScope(t *testing.T) {
	query := NewSymbolScope(ScopeQuery, nil, BoundSymbol{
		Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal,
	})
	lambda := NewSymbolScope(ScopeLambda, query, BoundSymbol{
		Kind: BoundLocal, Name: "amount", Type: TypeUnknown,
	})

	got, evidence, err := lambda.Resolve(Reference{Qualifier: "orders", Name: "amount"}, SourceSpan{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != BoundColumn || got.Qualifier != "orders" {
		t.Fatalf("selected=%#v", got)
	}
	if evidence.SelectedScope != ScopeQuery {
		t.Fatalf("scope=%s", evidence.SelectedScope)
	}
}

func TestSymbolScopeAmbiguityIsDeterministicAtNearestScope(t *testing.T) {
	external := NewSymbolScope(ScopeExternal, nil, BoundSymbol{
		Kind: BoundColumn, Qualifier: "legacy", Name: "id", Type: TypeInteger,
	})
	query := NewSymbolScope(ScopeQuery, external,
		BoundSymbol{Kind: BoundColumn, Qualifier: "orders", Name: "id", Type: TypeInteger},
		BoundSymbol{Kind: BoundColumn, Qualifier: "customers", Name: "id", Type: TypeInteger},
	)

	_, evidence, err := query.Resolve(Reference{Name: "id"}, SourceSpan{Start: 2, End: 4})
	bindingErr, ok := err.(*ScopeBindingError)
	if !ok || bindingErr.Code != ScopeBindingAmbiguous {
		t.Fatalf("err=%T %v", err, err)
	}
	if evidence.Found || evidence.Reason != BindingReasonAmbiguous {
		t.Fatalf("evidence=%#v", evidence)
	}
	if len(evidence.Candidates) != 2 {
		t.Fatalf("candidates=%#v", evidence.Candidates)
	}
	if evidence.Candidates[0].Symbol.Qualifier != "customers" || evidence.Candidates[1].Symbol.Qualifier != "orders" {
		t.Fatalf("candidate order=%#v", evidence.Candidates)
	}
	if evidence.Span.Start != 2 || evidence.Span.End != 4 {
		t.Fatalf("span=%#v", evidence.Span)
	}
}

func TestSymbolScopeUnboundRetainsReferenceAndSpan(t *testing.T) {
	scope := NewSymbolScope(ScopeMetric, nil, BoundSymbol{Kind: BoundMetric, Name: "revenue", Type: TypeDecimal})
	ref := Reference{Name: "missing"}
	span := SourceSpan{Start: 7, End: 14}

	_, evidence, err := scope.Resolve(ref, span)
	bindingErr, ok := err.(*ScopeBindingError)
	if !ok || bindingErr.Code != ScopeBindingUnbound {
		t.Fatalf("err=%T %v", err, err)
	}
	if evidence.Reference != ref || evidence.Span != span || evidence.Reason != BindingReasonUnbound {
		t.Fatalf("evidence=%#v", evidence)
	}
	if len(evidence.Candidates) != 0 || evidence.Found {
		t.Fatalf("evidence=%#v", evidence)
	}
}

func TestSymbolScopeOwnsInputSymbols(t *testing.T) {
	symbols := []BoundSymbol{{Kind: BoundMetric, Name: "revenue", Type: TypeDecimal}}
	scope := NewSymbolScope(ScopeMetric, nil, symbols...)
	symbols[0].Name = "mutated"

	got, _, err := scope.Resolve(Reference{Name: "revenue"}, SourceSpan{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "revenue" {
		t.Fatalf("selected=%#v", got)
	}

	owned := scope.Symbols()
	owned[0].Name = "changed"
	got, _, err = scope.Resolve(Reference{Name: "revenue"}, SourceSpan{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "revenue" {
		t.Fatalf("selected=%#v", got)
	}
}
