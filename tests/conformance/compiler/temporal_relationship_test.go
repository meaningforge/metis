package compiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestTemporalRelationshipLowersPointInTimeJoinAcrossTargets(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			plan, sqlQuery := compileTemporalRelationship(t, query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "customer_tier"}}}, dialect)
			if len(plan.Joins) != 1 || plan.Joins[0].Temporal == nil {
				t.Fatalf("temporal join plan = %#v", plan.Joins)
			}
			for _, fragment := range []string{"customer_history", "order_date", "valid_from", "valid_to", "COALESCE", "TRUE", ">=", "<"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s temporal SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
		})
	}
}

func TestTemporalRelationshipSemanticsSurviveReverseJoinTraversal(t *testing.T) {
	plan, sqlQuery := compileTemporalRelationship(t, query.SemanticQuery{Dimensions: []query.DimensionRef{{Name: "customer_tier"}, {Name: "status"}}}, "DORIS")
	if plan.Root.Name != "customer_history" {
		t.Fatalf("root = %q, want customer_history", plan.Root.Name)
	}
	if len(plan.Joins) != 1 || plan.Joins[0].FromDataset != "customer_history" || plan.Joins[0].ToDataset != "orders" || plan.Joins[0].Temporal == nil {
		t.Fatalf("reverse temporal join = %#v", plan.Joins)
	}
	for _, fragment := range []string{"order_date", "valid_from", "valid_to", "COALESCE"} {
		if !strings.Contains(sqlQuery.SQL, fragment) {
			t.Fatalf("reverse temporal SQL missing %q:\n%s", fragment, sqlQuery.SQL)
		}
	}
}

func compileTemporalRelationship(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SQLQuery) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	model.Datasets = append(model.Datasets, ossie.Dataset{Name: "customer_history", Source: "analytics.customer_history", Fields: []ossie.Field{
		{Name: "customer_id", Datatype: ossie.DataTypeString, Expression: temporalTestExpr("customer_history.customer_id")},
		{Name: "valid_from", Datatype: ossie.DataTypeDateTime, Expression: temporalTestExpr("customer_history.valid_from")},
		{Name: "valid_to", Datatype: ossie.DataTypeDateTime, Expression: temporalTestExpr("customer_history.valid_to")},
		{Name: "customer_tier", Datatype: ossie.DataTypeString, Expression: temporalTestExpr("customer_history.customer_tier"), Dimension: &ossie.Dimension{}},
	}})
	model.Relationships = append(model.Relationships, ossie.Relationship{
		Name: "orders_to_customer_history", From: "orders", To: "customer_history", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"},
		CustomExtensions: []ossie.CustomExtension{{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"temporal","from_time_dimension":"order_date","to_valid_from":"valid_from","to_valid_to":"valid_to","cardinality":"many_to_one"}`}},
	})
	if err := ossie.ValidateDocument(doc); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	query.Project, query.Model = projectName, modelName
	renderer := mustRenderer(t, dialect)
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query, renderer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	sqlQuery, err := compilePlan(context.Background(), plan, renderer)
	if err != nil {
		t.Fatal(err)
	}
	return plan, sqlQuery
}

func temporalTestExpr(sql string) ossie.Expression {
	return ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: sql}}}
}
