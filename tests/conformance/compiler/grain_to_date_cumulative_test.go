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

func TestGrainToDateCumulativeCompilerConformance(t *testing.T) {
	resetFragments := map[string]string{
		"DUCKDB":     "DATE_TRUNC('YEAR'",
		"DORIS":      "DATE_TRUNC(\"year\"",
		"CLICKHOUSE": "toStartOfYear(",
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			grain := query.TimeGrainMonth
			query := query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "ytd_revenue"}},
				Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
				Filters:    []query.Filter{{Field: "order_date", Operator: query.FilterGTE, Value: "2026-03-01"}},
			}
			plan, sqlQuery := compileGrainToDateWithTimeSpine(t, query, dialect)
			if plan.DenseCalendar == nil {
				t.Fatal("grain-to-date cumulative must plan a dense calendar")
			}
			if len(plan.Nodes) == 0 {
				t.Fatal("plan owns no semantic nodes")
			}
			if got := len(plan.Output.Predicates); got != 1 {
				t.Fatalf("post-evaluation predicates = %d, want visible range predicate", got)
			}

			base := semanticGraphNodeByID(t, plan.Nodes, "revenue")
			predicates := semanticGraphNodePredicates(base)
			if len(predicates) != 1 || predicates[0].Filter.Value != "2026-01-01" {
				t.Fatalf("grain-to-date dependency read range = %#v, want reset start 2026-01-01", predicates)
			}
			for _, fragment := range []string{"calendar", "COALESCE", resetFragments[dialect], "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s grain-to-date SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			if got := len(sqlQuery.Parameters); got != 2 {
				t.Fatalf("%s parameters = %d, want reset read bound plus visible bound", dialect, got)
			}
			if sqlQuery.Parameters[0].Value != "2026-01-01" || sqlQuery.Parameters[1].Value != "2026-03-01" {
				t.Fatalf("%s parameters = %#v", dialect, sqlQuery.Parameters)
			}
		})
	}
}

func semanticGraphNodeByID(t *testing.T, nodes []semanticplan.SemanticPlanNode, id string) semanticplan.SemanticPlanNode {
	t.Helper()
	for _, node := range nodes {
		if node.NodeBase().ID == id {
			return node
		}
	}
	t.Fatalf("semantic node %q not found", id)
	return nil
}

func semanticGraphNodePredicates(node semanticplan.SemanticPlanNode) []semanticplan.Predicate {
	var out []semanticplan.Predicate
	for _, owned := range node.NodeBase().Predicates {
		if owned.Predicate != nil {
			out = append(out, *owned.Predicate)
		}
	}
	return out
}

func compileGrainToDateWithTimeSpine(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SqlRenderResult) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	model.Metrics = append(model.Metrics, ossie.Metric{
		Name:       "ytd_revenue",
		Datatype:   ossie.DataTypeDecimal,
		Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "revenue"}}},
		CustomExtensions: []ossie.CustomExtension{
			{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"grain_to_date","unit":"year"}}`},
			{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"time_binding","time_dimension":"order_date"}`},
		},
	})
	model.CustomExtensions = append(model.CustomExtensions, ossie.CustomExtension{
		VendorName: ossie.MetisExtensionVendor,
		Data:       `{"kind":"time_spine","dataset":"calendar","time_dimension":"day","grains":["day","week","month","quarter","year"]}`,
	})
	if err := ossie.ValidateDocument(doc); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	query.Project = projectName
	query.Model = modelName
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
