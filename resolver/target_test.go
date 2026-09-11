package resolver_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/query"
)

func TestResolveForRendererSelectsPhysicalExpressionWithoutChangingSemantics(t *testing.T) {
	r := newResolverFromYAML(t, singleDatasetModel)
	q := query.SemanticQuery{Model: "single", Metrics: []query.MetricRef{{Name: "paid_revenue"}}}

	clickhouse, err := r.inner.ResolveForRenderer(context.Background(), withProject(q), mustRenderer(t, "CLICKHOUSE"))
	if err != nil {
		t.Fatal(err)
	}
	doris, err := r.inner.ResolveForRenderer(context.Background(), withProject(q), mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	duckdb, err := r.inner.ResolveForRenderer(context.Background(), withProject(q), mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}

	if clickhouse.RootDataset != doris.RootDataset || duckdb.RootDataset != doris.RootDataset || clickhouse.RootDataset != "orders" {
		t.Fatalf("target changed semantic root: clickhouse=%q doris=%q duckdb=%q", clickhouse.RootDataset, doris.RootDataset, duckdb.RootDataset)
	}
	if len(clickhouse.Relationships) != len(doris.Relationships) || len(duckdb.Relationships) != len(doris.Relationships) {
		t.Fatalf("target changed relationships: clickhouse=%d doris=%d duckdb=%d", len(clickhouse.Relationships), len(doris.Relationships), len(duckdb.Relationships))
	}
	if got := clickhouse.Metrics[0].Expression.Source; got != "sumIf(amount, status = 'paid')" {
		t.Fatalf("clickhouse selected expression = %q", got)
	}
	if got := doris.Metrics[0].Expression.Source; got != "SUM(CASE WHEN status = 'paid' THEN amount END)" {
		t.Fatalf("doris fallback expression = %q", got)
	}
	if got := duckdb.Metrics[0].Expression.Source; got != "SUM(CASE WHEN status = 'paid' THEN amount END)" {
		t.Fatalf("duckdb expression = %q", got)
	}
	if clickhouse.Metrics[0].Expression.Analysis == nil || doris.Metrics[0].Expression.Analysis == nil {
		t.Fatal("resolved metric expressions must retain SemanticManifest analysis")
	}
}

func withProject(q query.SemanticQuery) query.SemanticQuery {
	q.Project = testProject
	return q
}
