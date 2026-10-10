package resolver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
          - name: order_number
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.order_number}]
          - name: score
            datatype: Float
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.score}]
          - name: active
            datatype: Boolean
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.active}]
          - name: order_date
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.order_date}]
            dimension: {}
          - name: order_time
            datatype: Time
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.order_time}]
          - name: created_at
            datatype: DateTime
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.created_at}]
          - name: occurred_at
            datatype: DateTimeTz
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.occurred_at}]
          - name: native_value
            datatype: Opaque
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.native_value}]
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
          - name: vip
            datatype: Boolean
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer.vip}]
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

func TestResolveParsesStringFilterOperandsByDatatype(t *testing.T) {
	r := newResolver(t)
	var q query.SemanticQuery
	body := `{"project":"ignored","model":"sales","metrics":[{"name":"total_revenue"}],"filters":{"kind":"and","children":[` +
		`{"kind":"filter","filter":{"field":"order_number","operator":"eq","value":"9007199254740993"}},` +
		`{"kind":"filter","filter":{"field":"amount","operator":"between","value":["0.10000000000000000001","1e3"]}}]}}`
	if err := json.Unmarshal([]byte(body), &q); err != nil {
		t.Fatal(err)
	}
	resolved, err := r.Resolve(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Filters[0].Filter.Value; got != int64(9007199254740993) {
		t.Fatalf("integer operand = %#v", got)
	}
	values, ok := resolved.Filters[1].Filter.Value.([]any)
	if !ok || !reflect.DeepEqual(values, []any{json.Number("0.10000000000000000001"), json.Number("1000")}) {
		t.Fatalf("decimal operands = %#v", resolved.Filters[1].Filter.Value)
	}
}

func TestResolveConvertsBooleanFloatAndStringLiterals(t *testing.T) {
	r := newResolver(t)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
		Filters: query.Predicate{
			query.Leaf("active", query.FilterEQ, "true"),
			query.Leaf("score", query.FilterGTE, "1.25"),
			query.Leaf("order_id", query.FilterEQ, "123"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]any, len(resolved.Filters))
	for _, filter := range resolved.Filters {
		values[filter.Filter.Field] = filter.Filter.Value
	}
	if values["active"] != true || values["score"] != float64(1.25) || values["order_id"] != "123" {
		t.Fatalf("typed values = %#v", values)
	}
}

func TestResolveRejectsDatatypeIncompatibleFilterOperandsWithoutDisclosure(t *testing.T) {
	r := newResolver(t)
	for _, tc := range []struct {
		field   string
		operand string
	}{
		{"order_number", `"1.5"`},
		{"order_number", `"9223372036854775808"`},
		{"amount", `"1e38"`},
		{"amount", `"1e-39"`},
		{"order_date", `"20260101"`},
		{"active", `"1"`},
		{"score", `"1e309"`},
	} {
		var q query.SemanticQuery
		body := `{"model":"sales","metrics":[{"name":"total_revenue"}],"filters":{"kind":"filter","filter":{"field":"` + tc.field + `","operator":"eq","value":` + tc.operand + `}}}`
		if err := json.Unmarshal([]byte(body), &q); err != nil {
			t.Fatalf("decode %s: %v", tc.field, err)
		}
		_, err := r.Resolve(context.Background(), q)
		var semanticErr *serrors.Error
		if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidFilterValue {
			t.Fatalf("%s error = %v", tc.field, err)
		}
		if strings.Contains(err.Error(), tc.operand) {
			t.Fatalf("error disclosed operand: %v", err)
		}
	}
}

func TestResolveRejectsDatatypeIncompatibleFilterOperators(t *testing.T) {
	r := newResolver(t)
	for _, filter := range []query.Filter{
		{Field: "active", Operator: query.FilterGT, Value: "true"},
		{Field: "active", Operator: query.FilterBetween, Value: []string{"false", "true"}},
		{Field: "native_value", Operator: query.FilterLTE, Value: "vendor-value"},
		{Field: "native_value", Operator: query.FilterBetween, Value: []string{"a", "z"}},
	} {
		_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: []query.Filter{filter}})
		assertErrorCode(t, err, serrors.ErrInvalidFilterValue)
	}
}

func TestResolveRejectsDatatypeIncompatibleOperatorsAcrossPredicateForms(t *testing.T) {
	r := newResolver(t)
	queries := []query.SemanticQuery{
		{
			Model:   "sales",
			Metrics: []query.MetricRef{{Name: "total_revenue"}},
			Filters: query.Predicate{query.Logical(query.PredicateOr,
				query.Leaf("active", query.FilterGT, "true"),
				query.Leaf("active", query.FilterEQ, "true"),
			)},
		},
		{
			Model:   "sales",
			Metrics: []query.MetricRef{{Name: "total_revenue"}},
			Filters: query.Predicate{query.Exists("orders_to_customer",
				query.Leaf("customer.vip", query.FilterLT, "true"),
			)},
		},
	}
	for _, semanticQuery := range queries {
		_, err := r.Resolve(context.Background(), semanticQuery)
		assertErrorCode(t, err, serrors.ErrInvalidFilterValue)
	}
}

func TestResolveAcceptsSupportedBooleanAndOpaqueFilterOperators(t *testing.T) {
	r := newResolver(t)
	filters := []query.Filter{
		{Field: "active", Operator: query.FilterEQ, Value: "true"},
		{Field: "active", Operator: query.FilterIN, Value: []string{"false", "true"}},
		{Field: "active", Operator: query.FilterIsNotNull},
		{Field: "native_value", Operator: query.FilterEQ, Value: "vendor-value"},
		{Field: "native_value", Operator: query.FilterNotIn, Value: []string{"a", "b"}},
		{Field: "native_value", Operator: query.FilterIsNull},
	}
	if _, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: filters}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveValidatesTemporalFilterLiterals(t *testing.T) {
	r := newResolver(t)
	valid := []query.Filter{
		{Field: "order_date", Operator: query.FilterEQ, Value: "2026-09-09"},
		{Field: "order_time", Operator: query.FilterBetween, Value: []string{"12:30:45", "12:30:45.123456789"}},
		{Field: "created_at", Operator: query.FilterGTE, Value: "2026-09-09T12:30:45.123"},
		{Field: "created_at", Operator: query.FilterLT, Value: "2026-09-09T12:30:45Z"},
		{Field: "occurred_at", Operator: query.FilterLT, Value: "2026-09-09T12:30:45+08:00"},
	}
	if _, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: valid}); err != nil {
		t.Fatal(err)
	}

	for _, filter := range []query.Filter{
		{Field: "order_date", Operator: query.FilterEQ, Value: "2026-02-30"},
		{Field: "order_time", Operator: query.FilterEQ, Value: "25:00:00"},
		{Field: "created_at", Operator: query.FilterEQ, Value: "2026-09-09 12:30:45"},
		{Field: "occurred_at", Operator: query.FilterEQ, Value: "2026-09-09T12:30:45"},
		{Field: "order_date", Operator: query.FilterIN, Value: []string{"2026-09-09", "not-a-date"}},
	} {
		_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: []query.Filter{filter}})
		assertErrorCode(t, err, serrors.ErrInvalidFilterValue)
		if strings.Contains(err.Error(), fmt.Sprint(filter.Value)) {
			t.Fatalf("error disclosed operand: %v", err)
		}
	}
}

func TestResolveBooleanPredicateSameDataset(t *testing.T) {
	r := newResolver(t)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
		Filters: query.Predicate{query.Logical(query.PredicateOr,
			query.Leaf("region", query.FilterEQ, "APAC"),
			query.Leaf("region", query.FilterEQ, "EMEA"),
		)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.BooleanFilters) != 1 || resolved.BooleanFilters[0].Kind != query.PredicateOr || len(resolved.BooleanFilters[0].Children) != 2 {
		t.Fatalf("unexpected boolean filters: %#v", resolved.BooleanFilters)
	}
}

func TestResolveRelationshipExistencePredicate(t *testing.T) {
	r := newResolver(t)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
		Filters: query.Predicate{query.Exists("orders_to_customer", query.Logical(query.PredicateOr,
			query.Leaf("customer.region", query.FilterEQ, "APAC"),
			query.Leaf("customer.region", query.FilterEQ, "EMEA"),
		))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.RelationshipExistence) != 1 {
		t.Fatalf("relationship existence = %#v", resolved.RelationshipExistence)
	}
	existence := resolved.RelationshipExistence[0]
	if existence.Relationship.Name != "orders_to_customer" || existence.SourceDataset != "orders" || existence.TargetDataset != "customer" {
		t.Fatalf("relationship existence direction = %#v", existence)
	}
	if !reflect.DeepEqual(existence.SourceColumns, []string{"customer_id"}) || !reflect.DeepEqual(existence.TargetColumns, []string{"customer_id"}) {
		t.Fatalf("relationship existence correlations = %#v", existence)
	}
	if existence.Predicate.Kind != query.PredicateOr || len(existence.Predicate.Children) != 2 {
		t.Fatalf("relationship existence target predicate = %#v", existence.Predicate)
	}
}

func TestResolveRelationshipExistenceRejectsUnsafeShapes(t *testing.T) {
	r := newResolver(t)
	tests := []struct {
		name    string
		filters query.Predicate
	}{
		{name: "source field", filters: query.Predicate{query.Exists("orders_to_customer", query.Leaf("orders.order_date", query.FilterEQ, "2026-01-01"))}},
		{name: "metric target", filters: query.Predicate{query.Exists("orders_to_customer", query.Leaf("total_revenue", query.FilterGT, "0"))}},
		{name: "nested under or", filters: query.Predicate{query.Logical(query.PredicateOr,
			query.Exists("orders_to_customer", query.Leaf("customer.region", query.FilterEQ, "APAC")),
			query.Leaf("orders.order_date", query.FilterEQ, "2026-01-01"),
		)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: tc.filters})
			var apiErr *serrors.Error
			if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedQueryShape {
				t.Fatalf("error = %#v, want %s", err, serrors.ErrUnsupportedQueryShape)
			}
		})
	}
}

func TestResolveRelationshipExistenceRejectsTargetOutput(t *testing.T) {
	r := newResolver(t)
	_, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:      "sales",
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "customer.region"}},
		Filters:    query.Predicate{query.Exists("orders_to_customer", query.Leaf("customer.region", query.FilterEQ, "APAC"))},
	})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedQueryShape {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrUnsupportedQueryShape)
	}
}

func TestResolveRootAndStagesBooleanAndMetricLeavesIndependently(t *testing.T) {
	r := newResolver(t)
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
		Filters: query.Predicate{query.Logical(query.PredicateAnd,
			query.Logical(query.PredicateOr,
				query.Leaf("region", query.FilterEQ, "APAC"),
				query.Leaf("region", query.FilterEQ, "EMEA"),
			),
			query.Leaf("total_revenue", query.FilterGT, "100"),
		)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.BooleanFilters) != 1 || len(resolved.Filters) != 1 || resolved.Filters[0].Kind != resolver.FilterTargetMetric {
		t.Fatalf("unexpected staged filters: boolean=%#v flat=%#v", resolved.BooleanFilters, resolved.Filters)
	}
}

func TestResolveBooleanPredicateRejectsUnsafeStagesAndDatasets(t *testing.T) {
	r := newResolver(t)
	for name, predicate := range map[string]query.Filter{
		"cross_dataset": query.Logical(query.PredicateOr,
			query.Leaf("orders.customer_id", query.FilterEQ, "1"),
			query.Leaf("region", query.FilterEQ, "APAC"),
		),
		"time": query.Logical(query.PredicateNot,
			query.Leaf("order_date", query.FilterGTE, "2026-01-01"),
		),
		"metric": query.Logical(query.PredicateNot,
			query.Leaf("total_revenue", query.FilterGT, "10"),
		),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Filters: query.Predicate{predicate}})
			if err == nil {
				t.Fatal("expected boolean predicate to be rejected")
			}
		})
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
