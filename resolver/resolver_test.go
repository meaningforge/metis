package resolver_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

const testProject = "resolver-test"

const semanticModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.public.orders
        fields:
          - name: order_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.order_id}]
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.customer_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: order_date
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.order_date}]
            dimension: {}
      - name: customer
        source: sales.public.customer
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer.customer_id}]
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer.region}]
            dimension: {}
    relationships:
      - name: orders_to_customer
        from: orders
        to: customer
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`

const singleDatasetModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: single
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
          - name: status
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: status}]
    metrics:
      - name: paid_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"}
            - {dialect: CLICKHOUSE, expression: "sumIf(amount, status = 'paid')"}
`

const ambiguousRelationshipModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: ambiguous
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.id}]}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}}
      - name: customer
        source: analytics.customer
        fields:
          - {name: id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: customer.id}]}}
      - name: store
        source: analytics.store
        fields:
          - {name: id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: store.id}]}}
      - name: region
        source: analytics.region
        fields:
          - {name: id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: region.id}]}}
          - name: name
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: region.name}]}
            dimension: {}
    relationships:
      - {name: orders_to_store, from: orders, to: store, from_columns: [id], to_columns: [id]}
      - {name: store_to_region, from: store, to: region, from_columns: [id], to_columns: [id]}
      - {name: orders_to_customer, from: orders, to: customer, from_columns: [id], to_columns: [id]}
      - {name: customer_to_region, from: customer, to: region, from_columns: [id], to_columns: [id]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
`

type testResolver struct{ inner *resolver.Resolver }

func newResolver(t *testing.T) *testResolver {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(semanticModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(testProject, doc)
	if err != nil {
		t.Fatal(err)
	}
	return &testResolver{inner: resolver.New(manifest.NewStore(snapshot))}
}

func newResolverFromYAML(t *testing.T, content string) *testResolver {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(testProject, doc)
	if err != nil {
		t.Fatal(err)
	}
	return &testResolver{inner: resolver.New(manifest.NewStore(snapshot))}
}

func (r *testResolver) Resolve(ctx context.Context, q query.SemanticQuery) (*resolver.ResolvedSemanticQuery, error) {
	q.Project = testProject
	return r.inner.Resolve(ctx, q)
}

func (r *testResolver) ResolveForRenderer(ctx context.Context, q query.SemanticQuery, renderer renderer.Renderer) (*resolver.ResolvedSemanticQuery, error) {
	q.Project = testProject
	return r.inner.ResolveForRenderer(ctx, q, renderer)
}

func TestResolveMetricAndDimensionAcrossRelationship(t *testing.T) {
	r := newResolver(t)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Dimensions: []query.DimensionRef{{Name: "region"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RootDataset != "orders" {
		t.Fatalf("root dataset = %q, want orders", resolved.RootDataset)
	}
	if len(resolved.Relationships) != 1 || resolved.Relationships[0].Name != "orders_to_customer" {
		t.Fatalf("unexpected relationships: %#v", resolved.Relationships)
	}
	if len(resolved.Dimensions) != 1 || resolved.Dimensions[0].Dataset != "customer" {
		t.Fatalf("unexpected dimension resolution: %#v", resolved.Dimensions)
	}
}

func TestResolveRejectsAmbiguousRelationshipPathWithCandidates(t *testing.T) {
	r := newResolverFromYAML(t, ambiguousRelationshipModel)
	_, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:      "ambiguous",
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "region.name"}},
	})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrAmbiguousRelationshipPath {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrAmbiguousRelationshipPath)
	}
	want := [][]string{
		{"orders_to_customer", "customer_to_region"},
		{"orders_to_store", "store_to_region"},
	}
	if got := apiErr.Details["relationship_paths"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("relationship paths = %#v, want %#v", got, want)
	}
}

func TestResolveUnqualifiedMetricExpressionInSingleDatasetModel(t *testing.T) {
	r := newResolverFromYAML(t, singleDatasetModel)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "single", Metrics: []query.MetricRef{{Name: "paid_revenue"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RootDataset != "orders" {
		t.Fatalf("root dataset = %q, want orders", resolved.RootDataset)
	}
	if len(resolved.Metrics) != 1 || len(resolved.Metrics[0].Datasets) != 1 || resolved.Metrics[0].Datasets[0] != "orders" {
		t.Fatalf("unexpected metric datasets: %#v", resolved.Metrics)
	}
}

func TestResolveTimeDimensionGrain(t *testing.T) {
	r := newResolver(t)
	grain := query.TimeGrainMonth
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Dimensions[0].Grain == nil || *resolved.Dimensions[0].Grain != query.TimeGrainMonth {
		t.Fatalf("unexpected grain: %#v", resolved.Dimensions[0].Grain)
	}
}

func TestResolveOrderByMetricAndDimension(t *testing.T) {
	r := newResolver(t)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, OrderBy: []query.OrderBy{{Field: "total_revenue", Direction: query.SortDesc}, {Field: "region", Direction: query.SortAsc}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.OrderBy) != 2 {
		t.Fatalf("unexpected order_by count: %d", len(resolved.OrderBy))
	}
	if resolved.OrderBy[0].Kind != resolver.OrderTargetMetric || resolved.OrderBy[0].Metric == nil {
		t.Fatalf("metric order_by not resolved: %#v", resolved.OrderBy[0])
	}
	if resolved.OrderBy[1].Kind != resolver.OrderTargetDimension || resolved.OrderBy[1].Dataset != "customer" {
		t.Fatalf("dimension order_by not resolved: %#v", resolved.OrderBy[1])
	}
	if len(resolved.Relationships) != 1 || resolved.Relationships[0].Name != "orders_to_customer" {
		t.Fatalf("order_by dimension should require customer relationship: %#v", resolved.Relationships)
	}
}

func TestResolveFilterValueShapes(t *testing.T) {
	r := newResolver(t)
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: []query.Filter{{Field: "region", Operator: query.FilterIN, Value: []string{"JP", "CN"}}, {Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-01-01", "2026-06-30"}}, {Field: "orders.customer_id", Operator: query.FilterIsNotNull}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRejectInvalidFilterValueShape(t *testing.T) {
	r := newResolver(t)
	cases := []query.Filter{{Field: "region", Operator: query.FilterIN, Value: "JP"}, {Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-01-01"}}, {Field: "orders.customer_id", Operator: query.FilterIsNull, Value: "unexpected"}, {Field: "region", Operator: query.FilterEQ, Value: []string{"JP"}}, {Field: "region", Operator: query.FilterOperator("contains"), Value: "JP"}}
	for _, filter := range cases {
		_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: []query.Filter{filter}})
		assertErrorCode(t, err, serrors.ErrInvalidQuery)
	}
}

func TestRejectAmbiguousFilterField(t *testing.T) {
	r := newResolver(t)
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: []query.Filter{{Field: "customer_id", Operator: query.FilterIsNotNull}}})
	assertErrorCode(t, err, serrors.ErrAmbiguousField)
}

func TestRejectInvalidOrderBy(t *testing.T) {
	r := newResolver(t)
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, OrderBy: []query.OrderBy{{Field: "unknown", Direction: query.SortDesc}}})
	assertErrorCode(t, err, serrors.ErrInvalidQuery)
	_, err = r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, OrderBy: []query.OrderBy{{Field: "total_revenue", Direction: query.SortDirection("sideways")}}})
	assertErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestRejectInvalidLimit(t *testing.T) {
	r := newResolver(t)
	limit := 0
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Limit: &limit})
	assertErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestRejectGrainOnNonTimeDimension(t *testing.T) {
	r := newResolver(t)
	grain := query.TimeGrainMonth
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Dimensions: []query.DimensionRef{{Name: "region", Grain: &grain}}})
	assertErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestRejectMissingMetric(t *testing.T) {
	r := newResolver(t)
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "missing"}}})
	assertErrorCode(t, err, serrors.ErrMetricNotFound)
}

func assertErrorCode(t *testing.T, err error, code serrors.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != code {
		t.Fatalf("unexpected error: %v", err)
	}
}
