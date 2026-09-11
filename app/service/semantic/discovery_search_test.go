package semantic

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestSearchSemanticsFiltersByKind(t *testing.T) {
	result, err := newService(t).SearchSemantics(context.Background(), SearchSemanticsRequest{
		Project: "finance",
		Query:   "revenue",
		Kinds:   []AssetKind{AssetMetric},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Kind != AssetMetric || result.Matches[0].Name != "total_revenue" {
		t.Fatalf("matches = %#v", result.Matches)
	}
}

func TestSearchSemanticsFiltersByModel(t *testing.T) {
	result, err := newService(t).SearchSemantics(context.Background(), SearchSemanticsRequest{
		Project: "finance",
		Query:   "revenue",
		Model:   "sales",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) == 0 {
		t.Fatal("expected model-scoped matches")
	}
	for _, match := range result.Matches {
		if match.Model != "sales" {
			t.Fatalf("model-scoped match = %#v", match)
		}
	}
}

func TestSearchSemanticsAppliesDeterministicLimit(t *testing.T) {
	req := SearchSemanticsRequest{Project: "finance", Query: "revenue", Limit: 1}
	first, err := newService(t).SearchSemantics(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newService(t).SearchSemantics(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Matches) != 1 || len(second.Matches) != 1 || !reflect.DeepEqual(first.Matches[0], second.Matches[0]) {
		t.Fatalf("limited results are not deterministic: first=%#v second=%#v", first.Matches, second.Matches)
	}
}

func TestSearchSemanticsRejectsInvalidFilters(t *testing.T) {
	for _, req := range []SearchSemanticsRequest{
		{Project: "finance", Query: "revenue", Kinds: []AssetKind{"unknown"}},
		{Project: "finance", Query: "revenue", Limit: MaxSearchLimit + 1},
	} {
		_, err := newService(t).SearchSemantics(context.Background(), req)
		var metisErr *serrors.Error
		if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrInvalidQuery {
			t.Fatalf("error = %#v, want %s", err, serrors.ErrInvalidQuery)
		}
	}
}

func TestSearchSemanticsRejectsUnknownModelFilter(t *testing.T) {
	_, err := newService(t).SearchSemantics(context.Background(), SearchSemanticsRequest{
		Project: "finance",
		Query:   "revenue",
		Model:   "missing",
	})
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrModelNotFound {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrModelNotFound)
	}
}
