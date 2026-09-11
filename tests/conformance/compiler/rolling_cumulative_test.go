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

func TestRollingCumulativeCompilerConformance(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			grain := query.TimeGrainMonth
			query := query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "rolling_3_month_revenue"}},
				Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
				Filters: []query.Filter{{
					Field:    "order_date",
					Operator: query.FilterGTE,
					Value:    "2026-03-01",
				}},
			}

			plan, sqlQuery := compileRollingWithTimeSpine(t, query, dialect)
			if plan.DenseCalendar == nil {
				t.Fatal("rolling cumulative must plan a dense calendar")
			}
			if len(plan.Nodes) == 0 {
				t.Fatal("plan owns no nodes")
			}
			if got := len(plan.Output.Predicates); got != 1 {
				t.Fatalf("post-evaluation predicates = %d, want visible range predicate", got)
			}

			var base semanticplan.SemanticPlanNode
			for _, node := range plan.Nodes {
				if node.NodeBase().ID == "revenue" {
					base = node
					break
				}
			}
			if base == nil {
				t.Fatal("revenue node not found")
			}
			var predicates []semanticplan.Predicate
			for _, predicate := range base.NodeBase().Predicates {
				if predicate.Predicate != nil {
					predicates = append(predicates, *predicate.Predicate)
				}
			}
			if len(predicates) != 1 || predicates[0].Filter.Value != "2026-01-01" {
				t.Fatalf("rolling dependency read range = %#v, want lower bound widened to 2026-01-01", predicates)
			}

			for _, fragment := range []string{"calendar", "COALESCE", "ROWS BETWEEN 2 PRECEDING AND CURRENT ROW"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s rolling cumulative SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			if got := len(sqlQuery.Parameters); got != 2 {
				t.Fatalf("%s parameters = %d, want widened read bound plus visible bound", dialect, got)
			}
		})
	}
}

func compileRollingWithTimeSpine(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SqlRenderResult) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	model.Metrics = append(model.Metrics, ossie.Metric{
		Name:       "rolling_3_month_revenue",
		Datatype:   ossie.DataTypeDecimal,
		Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "revenue"}}},
		CustomExtensions: []ossie.CustomExtension{
			{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"rolling","count":3,"unit":"month"}}`},
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
