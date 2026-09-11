package expression

import "testing"

type fixedReferenceResolver map[Reference]BoundSymbol

func (r fixedReferenceResolver) ResolveReferences(refs []Reference) (map[Reference]BoundSymbol, error) {
	out := make(map[Reference]BoundSymbol, len(refs))
	for _, ref := range refs {
		if symbol, ok := r[ref]; ok {
			out[ref] = symbol
		}
	}
	return out, nil
}

type sharedMapReferenceResolver struct {
	bindings map[Reference]BoundSymbol
}

func (r sharedMapReferenceResolver) ResolveReferences([]Reference) (map[Reference]BoundSymbol, error) {
	return r.bindings, nil
}

func TestBindRequiresResolverForReferences(t *testing.T) {
	ast, err := Parse("orders.amount", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(ast, nil); err == nil {
		t.Fatal("Bind with references and nil resolver unexpectedly succeeded")
	}
}

func TestBindRejectsMissingSymbol(t *testing.T) {
	ast, err := Parse("orders.amount + tax", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Bind(ast, fixedReferenceResolver{
		{Qualifier: "orders", Name: "amount"}: {Kind: BoundColumn, Qualifier: "orders", Name: "amount"},
	})
	if err == nil {
		t.Fatal("expected missing tax binding to fail")
	}
}

func TestBindOwnsReturnedBindings(t *testing.T) {
	ast, err := Parse("orders.amount", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	ref := Reference{Qualifier: "orders", Name: "amount"}
	resolved := map[Reference]BoundSymbol{
		ref: {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
	}
	bound, err := Bind(ast, sharedMapReferenceResolver{bindings: resolved})
	if err != nil {
		t.Fatal(err)
	}
	resolved[ref] = BoundSymbol{Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeString}
	if got := bound.Bindings[ref].Type; got != TypeDecimal {
		t.Fatalf("bound binding type = %s after resolver result mutation, want %s", got, TypeDecimal)
	}
}

func TestBoundExpressionResolveUsesCanonicalReferenceShape(t *testing.T) {
	bound := BoundExpression{Bindings: map[Reference]BoundSymbol{
		{Name: "tax"}:                         {Kind: BoundMetric, Name: "tax"},
		{Qualifier: "orders", Name: "amount"}: {Kind: BoundColumn, Qualifier: "orders", Name: "amount"},
	}}

	if got, ok := bound.Resolve([]string{"tax"}); !ok || got.Name != "tax" {
		t.Fatalf("Resolve(tax) = %#v, %v", got, ok)
	}
	if got, ok := bound.Resolve([]string{"orders", "amount"}); !ok || got.Qualifier != "orders" || got.Name != "amount" {
		t.Fatalf("Resolve(orders.amount) = %#v, %v", got, ok)
	}
	if _, ok := bound.Resolve(nil); ok {
		t.Fatal("Resolve(nil) unexpectedly succeeded")
	}
}

func TestBoundExpressionSymbolsAreDeterministic(t *testing.T) {
	bound := BoundExpression{Bindings: map[Reference]BoundSymbol{
		{Name: "tax"}:                            {Kind: BoundMetric, Name: "tax"},
		{Qualifier: "orders", Name: "status"}:    {Kind: BoundColumn, Qualifier: "orders", Name: "status"},
		{Qualifier: "orders", Name: "amount"}:    {Kind: BoundColumn, Qualifier: "orders", Name: "amount"},
		{Qualifier: "customers", Name: "region"}: {Kind: BoundColumn, Qualifier: "customers", Name: "region"},
	}}

	got := bound.Symbols()
	want := []string{"tax", "region", "amount", "status"}
	if len(got) != len(want) {
		t.Fatalf("Symbols() len = %d, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("Symbols()[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}
