package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestSemanticSearchCoversRankedSearchAndEnumeration(t *testing.T) {
	svc := NewSemanticSearchService(newService(t))
	searched, err := svc.Search(context.Background(), SemanticSearchRequest{
		Project: "finance",
		Query:   "revenue region",
		Kinds:   []AssetKind{AssetMetric, AssetDimension},
		Model:   "sales",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSemanticSearchItem(searched.Items, "metric:sales.total_revenue") || !hasSemanticSearchItem(searched.Items, "dimension:sales.customer.region") {
		t.Fatalf("searched items=%#v", searched.Items)
	}
	for _, item := range searched.Items {
		if item.Kind != AssetMetric && item.Kind != AssetDimension {
			t.Fatalf("multi-kind search leaked unexpected kind %q: %#v", item.Kind, searched.Items)
		}
	}
	encoded, err := json.Marshal(searched)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"score", "match_reasons"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("compact Agent search leaked %q: %s", forbidden, encoded)
		}
	}

	listed, err := svc.Search(context.Background(), SemanticSearchRequest{
		Project: "finance",
		Kinds:   []AssetKind{AssetMetric},
		Model:   "sales",
		Limit:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 || listed.Items[0].Ref != "metric:sales.gross_margin" || listed.Items[1].Ref != "metric:sales.total_cost" {
		t.Fatalf("enumerated items=%#v", listed.Items)
	}
}

func TestSemanticSearchCursorRejectsActivatedGeneration(t *testing.T) {
	discovery := newService(t).WithGenerationIdentity("finance", 7)
	service := NewSemanticSearchService(discovery)
	first, err := service.Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	discovery.WithGenerationIdentity("finance", 8)
	_, err = service.Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 1, Cursor: first.NextCursor})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrSemanticPaginationRestart {
		t.Fatalf("err=%v, want %s", err, serrors.ErrSemanticPaginationRestart)
	}
}

func TestSemanticSearchEnumerationCursorIsDeterministic(t *testing.T) {
	svc := NewSemanticSearchService(newService(t))
	first, err := svc.Search(context.Background(), SemanticSearchRequest{
		Project: "finance",
		Kinds:   []AssetKind{AssetMetric},
		Model:   "sales",
		Limit:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page=%#v", first)
	}
	second, err := svc.Search(context.Background(), SemanticSearchRequest{
		Project: "finance",
		Kinds:   []AssetKind{AssetMetric},
		Model:   "sales",
		Limit:   1,
		Cursor:  first.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Ref == first.Items[0].Ref {
		t.Fatalf("second page=%#v first=%#v", second, first)
	}
}

func TestSemanticSearchRejectsCursorInRankedMode(t *testing.T) {
	svc := NewSemanticSearchService(newService(t))
	_, err := svc.Search(context.Background(), SemanticSearchRequest{
		Project: "finance",
		Query:   "revenue",
		Cursor:  "opaque",
	})
	if err == nil || !strings.Contains(err.Error(), "cursor is only supported for enumeration") {
		t.Fatalf("err=%v", err)
	}
}

func hasSemanticSearchItem(items []SemanticSearchItem, ref string) bool {
	for _, item := range items {
		if item.Ref == ref {
			return true
		}
	}
	return false
}
