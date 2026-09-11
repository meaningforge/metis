package semantic

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestSemanticContextRepeatedRequestIsDeterministic(t *testing.T) {
	svc := newSemanticContextService(t)
	req := SemanticContextRequest{
		Project:    "finance",
		Model:      "sales",
		Metrics:    []string{"gross_margin"},
		Dimensions: []string{"region", "category"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Metric: "gross_margin",
			Limit:  1,
		},
	}

	first, err := svc.Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("semantic context changed across identical requests:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestSemanticContextPreservesFocusedOrderAndDeduplicates(t *testing.T) {
	result, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{
		Project:    "finance",
		Model:      "sales",
		Metrics:    []string{"total_cost", "total_revenue", "total_cost"},
		Dimensions: []string{"category", "region", "category"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := metricContextNames(result.Metrics), []string{"total_cost", "total_revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metric order = %v, want %v", got, want)
	}
	if got, want := dimensionContextNames(result.Dimensions), []string{"category", "region"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dimension order = %v, want %v", got, want)
	}
}

func TestSemanticContextIncludesOnlyRelationshipsNeededByFocusedEvidence(t *testing.T) {
	result, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{
		Project:    "finance",
		Model:      "sales",
		Metrics:    []string{"total_revenue"},
		Dimensions: []string{"region"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := relationshipNames(result.Relationships), []string{"orders_to_customer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("relationships = %v, want only focused evidence %v", got, want)
	}
}

func TestSemanticContextCompatibleDimensionsPageIsHardBounded(t *testing.T) {
	_, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Limit: maxCompatibleDimensionsLimit + 1,
		},
	})
	assertSemanticContextErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestSemanticContextCursorIsStableForRepeatedFirstPage(t *testing.T) {
	svc := newSemanticContextService(t)
	req := SemanticContextRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"gross_margin"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Metric: "gross_margin",
			Limit:  1,
		},
	}

	first, err := svc.Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.CompatibleDimensionsPage == nil || second.CompatibleDimensionsPage == nil {
		t.Fatalf("missing compatible dimensions page: first=%#v second=%#v", first, second)
	}
	if first.CompatibleDimensionsPage.NextCursor != second.CompatibleDimensionsPage.NextCursor {
		t.Fatalf("cursor changed across identical requests: %q != %q", first.CompatibleDimensionsPage.NextCursor, second.CompatibleDimensionsPage.NextCursor)
	}
}

func metricContextNames(metrics []MetricContext) []string {
	out := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		out = append(out, metric.Name)
	}
	return out
}

func dimensionContextNames(dimensions []DimensionContext) []string {
	out := make([]string, 0, len(dimensions))
	for _, dimension := range dimensions {
		out = append(out, dimension.Name)
	}
	return out
}
