package expression

import (
	"reflect"
	"testing"
)

func TestCollectReferencesDeduplicatesInEncounterOrder(t *testing.T) {
	expr := &BinaryExpr{
		Left: &IdentifierExpr{Parts: []string{"orders", "amount"}},
		Op:   "+",
		Right: &BinaryExpr{
			Left:  &IdentifierExpr{Parts: []string{"orders", "amount"}},
			Op:    "+",
			Right: &IdentifierExpr{Parts: []string{"tax"}},
		},
	}
	want := []Reference{{Qualifier: "orders", Name: "amount"}, {Name: "tax"}}
	if got := CollectReferences(expr); !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectReferences() = %#v, want %#v", got, want)
	}
}

func TestCollectReferencesExcludesLambdaLocalsCaseInsensitively(t *testing.T) {
	expr := &LambdaExpr{
		Params: []string{"Item"},
		Body: &BinaryExpr{
			Left:  &IdentifierExpr{Parts: []string{"item", "price"}},
			Op:    "+",
			Right: &IdentifierExpr{Parts: []string{"orders", "tax"}},
		},
	}
	want := []Reference{{Qualifier: "orders", Name: "tax"}}
	if got := CollectReferences(expr); !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectReferences() = %#v, want %#v", got, want)
	}
}

func TestCollectReferencesNestedLambdaScopesRestoreOuterLocal(t *testing.T) {
	expr := &LambdaExpr{
		Params: []string{"outer"},
		Body: &TupleExpr{Items: []Expr{
			&LambdaExpr{
				Params: []string{"inner"},
				Body: &BinaryExpr{
					Left:  &IdentifierExpr{Parts: []string{"outer"}},
					Op:    "+",
					Right: &IdentifierExpr{Parts: []string{"inner"}},
				},
			},
			&IdentifierExpr{Parts: []string{"outer"}},
			&IdentifierExpr{Parts: []string{"orders", "amount"}},
		}},
	}
	want := []Reference{{Qualifier: "orders", Name: "amount"}}
	if got := CollectReferences(expr); !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectReferences() = %#v, want %#v", got, want)
	}
}

func TestCollectReferencesIgnoresLiteralAndWildcard(t *testing.T) {
	expr := &TupleExpr{Items: []Expr{
		&LiteralExpr{Value: "42"},
		&WildcardExpr{},
	}}
	if got := CollectReferences(expr); len(got) != 0 {
		t.Fatalf("CollectReferences() = %#v, want no references", got)
	}
}
