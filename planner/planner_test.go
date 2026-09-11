package planner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

func TestPlanBuildsSemanticOperations(t *testing.T) {
	orders := &ossie.Dataset{Name: "orders", Source: "sales.public.orders"}
	customer := &ossie.Dataset{Name: "customer", Source: "sales.public.customer", PrimaryKey: []string{"customer_id"}}
	region := &ossie.Field{Name: "region", Dimension: &ossie.Dimension{}}
	orderDate := &ossie.Field{Name: "order_date", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{}}
	metric := &ossie.Metric{Name: "total_revenue", Datatype: ossie.DataTypeDecimal}
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	model := &ossie.SemanticModel{Name: "sales"}

	index := &manifest.ModelIndex{
		Model: model,
		Datasets: map[string]*ossie.Dataset{
			"orders":   orders,
			"customer": customer,
		},
		Metrics: map[string]*ossie.Metric{"total_revenue": metric},
		MetricDependencies: map[string]manifest.MetricDependency{
			"total_revenue": {DirectDatasets: []string{"orders"}, Datasets: []string{"orders"}},
		},
		Graph: manifest.NewRelationshipGraph([]*ossie.Relationship{rel}),
	}
	grain := query.TimeGrainMonth
	limit := 100
	resolved := &resolver.ResolvedSemanticQuery{
		Model:       index,
		RootDataset: "orders",
		Metrics: []resolver.ResolvedMetric{{
			Name: "total_revenue", Metric: metric, Datasets: []string{"orders"}, Expression: resolvedMetricExpression("SUM(orders.amount)"),
		}},
		EvaluationMetrics: []resolver.ResolvedMetric{{
			Name: "total_revenue", Metric: metric, Datasets: []string{"orders"}, Expression: resolvedMetricExpression("SUM(orders.amount)"),
		}},
		Dimensions: []resolver.ResolvedDimension{{
			Name: "region", Dataset: "customer", Field: region, Expression: resolvedFieldExpression("customer.region"),
		}, {
			Name: "order_date", Dataset: "orders", Field: orderDate, Grain: &grain, Expression: resolvedFieldExpression("orders.order_date"),
		}},
		Filters: []resolver.ResolvedFilter{{
			Filter: query.Filter{Field: "region", Operator: query.FilterEQ, Value: "JP"}, Kind: resolver.FilterTargetField, Dataset: "customer", Field: region, Expression: resolvedFieldExpression("customer.region"),
		}},
		Relationships: []*ossie.Relationship{rel},
		OrderBy: []resolver.ResolvedOrderBy{{
			Name: "total_revenue", Direction: query.SortDesc, Kind: resolver.OrderTargetMetric, Metric: metric, Expression: resolvedMetricExpression("SUM(orders.amount)"),
		}},
		Limit: &limit,
	}

	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Model.Name != "sales" || plan.Root.Name != "orders" || plan.Root.Source != "sales.public.orders" {
		t.Fatalf("unexpected model/root: %#v %#v", plan.Model, plan.Root)
	}
	if len(plan.Joins) != 1 || plan.Joins[0].FromDataset != "orders" || plan.Joins[0].ToDataset != "customer" {
		t.Fatalf("unexpected joins: %#v", plan.Joins)
	}
	if len(plan.Projections) != 3 {
		t.Fatalf("projection count = %d, want 3", len(plan.Projections))
	}
	if plan.Projections[0].Kind != semanticplan.ProjectionDimension || plan.Projections[2].Kind != semanticplan.ProjectionMetric {
		t.Fatalf("unexpected projection ordering/kinds: %#v", plan.Projections)
	}
	if len(plan.Groups) != 2 || plan.Groups[1].Grain == nil || *plan.Groups[1].Grain != query.TimeGrainMonth {
		t.Fatalf("unexpected groups: %#v", plan.Groups)
	}
	if len(plan.Predicates) != 1 || plan.Predicates[0].Dataset != "customer" {
		t.Fatalf("unexpected predicates: %#v", plan.Predicates)
	}
	if len(plan.Sorts) != 1 || plan.Sorts[0].Kind != semanticplan.SortMetric || plan.Sorts[0].Direction != query.SortDesc {
		t.Fatalf("unexpected sorts: %#v", plan.Sorts)
	}
	if plan.Limit == nil || *plan.Limit != 100 {
		t.Fatalf("unexpected limit: %#v", plan.Limit)
	}
}

func resolvedFieldExpression(source string) expression.ResolvedExpression {
	return expression.NewResolvedExpression("ANSI_SQL", source)
}

func resolvedMetricExpression(source string) expression.ResolvedExpression {
	return resolvedFieldExpression(source).WithAnalysis(expression.BoundExpression{}, expression.TypedExpression{})
}

func TestPlanJoinOrderIsDeterministicFromRoot(t *testing.T) {
	orders := &ossie.Dataset{Name: "orders", Source: "orders"}
	customer := &ossie.Dataset{Name: "customer", Source: "customer", PrimaryKey: []string{"customer_id"}}
	account := &ossie.Dataset{Name: "account", Source: "account", PrimaryKey: []string{"account_id"}}
	model := &ossie.SemanticModel{Name: "sales"}
	index := &manifest.ModelIndex{
		Model:    model,
		Datasets: map[string]*ossie.Dataset{"orders": orders, "customer": customer, "account": account},
	}
	// Deliberately supply relationships in reverse traversal order.
	customerToAccount := &ossie.Relationship{Name: "customer_to_account", From: "customer", To: "account", FromColumns: []string{"account_id"}, ToColumns: []string{"account_id"}}
	ordersToCustomer := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	resolved := &resolver.ResolvedSemanticQuery{
		Model: index, RootDataset: "orders",
		Relationships: []*ossie.Relationship{customerToAccount, ordersToCustomer},
	}

	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Joins) != 2 {
		t.Fatalf("join count = %d, want 2", len(plan.Joins))
	}
	if plan.Joins[0].FromDataset != "orders" || plan.Joins[0].ToDataset != "customer" {
		t.Fatalf("first join should start at root: %#v", plan.Joins[0])
	}
	if plan.Joins[1].FromDataset != "customer" || plan.Joins[1].ToDataset != "account" {
		t.Fatalf("second join should extend connected tree: %#v", plan.Joins[1])
	}
}

func TestPlanRejectsDisconnectedRelationshipSet(t *testing.T) {
	orders := &ossie.Dataset{Name: "orders", Source: "orders"}
	customer := &ossie.Dataset{Name: "customer", Source: "customer"}
	account := &ossie.Dataset{Name: "account", Source: "account"}
	index := &manifest.ModelIndex{
		Model:    &ossie.SemanticModel{Name: "sales"},
		Datasets: map[string]*ossie.Dataset{"orders": orders, "customer": customer, "account": account},
	}
	resolved := &resolver.ResolvedSemanticQuery{
		Model: index, RootDataset: "orders",
		Relationships: []*ossie.Relationship{{Name: "customer_to_account", From: "customer", To: "account"}},
	}

	_, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err == nil {
		t.Fatal("expected error")
	}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanRejectsUnresolvedExpressionBoundary(t *testing.T) {
	field := &ossie.Field{Name: "region", Dimension: &ossie.Dimension{}}
	index := &manifest.ModelIndex{
		Model:    &ossie.SemanticModel{Name: "sales"},
		Datasets: map[string]*ossie.Dataset{"orders": {Name: "orders", Source: "orders"}},
	}
	resolved := &resolver.ResolvedSemanticQuery{
		Model: index, RootDataset: "orders",
		Dimensions: []resolver.ResolvedDimension{{Name: "region", Dataset: "orders", Field: field}},
	}
	_, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrInternalInvariant {
		t.Fatalf("error = %#v, want unresolved-expression invariant violation", err)
	}
}

func TestPlanRejectsMetricWithoutSemanticAnalysis(t *testing.T) {
	metric := &ossie.Metric{Name: "revenue"}
	index := &manifest.ModelIndex{
		Model:    &ossie.SemanticModel{Name: "sales"},
		Datasets: map[string]*ossie.Dataset{"orders": {Name: "orders", Source: "orders"}},
	}
	resolved := &resolver.ResolvedSemanticQuery{
		Model: index, RootDataset: "orders",
		Metrics: []resolver.ResolvedMetric{{Name: "revenue", Metric: metric, Expression: resolvedFieldExpression("SUM(orders.amount)")}},
	}
	_, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrInternalInvariant {
		t.Fatalf("error = %#v, want missing-analysis invariant violation", err)
	}
}
