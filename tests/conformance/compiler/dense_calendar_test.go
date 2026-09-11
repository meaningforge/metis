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

func TestDenseCalendarLowersBeforeCumulativeWindow(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			grain := query.TimeGrainMonth
			q := query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "cumulative_revenue"}},
				Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
			}
			plan, sqlQuery := compileWithTimeSpine(t, q, dialect)
			if plan.DenseCalendar == nil {
				t.Fatal("dense calendar plan is nil")
			}
			for _, fragment := range []string{"analytics", "calendar", "COALESCE", "0", "OVER (", "UNBOUNDED PRECEDING"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s dense cumulative SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			calendarPos := strings.Index(sqlQuery.SQL, "calendar")
			windowPos := strings.Index(sqlQuery.SQL, "OVER (")
			if calendarPos < 0 || windowPos < 0 || calendarPos > windowPos {
				t.Fatalf("%s calendar densification must precede cumulative window:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}

func TestDenseCalendarPreservesAdditiveDimensionDomain(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			grain := query.TimeGrainMonth
			q := query.SemanticQuery{
				Metrics: []query.MetricRef{{Name: "cumulative_revenue"}},
				Dimensions: []query.DimensionRef{
					{Name: "order_date", Grain: &grain},
					{Name: "region"},
				},
			}
			_, sqlQuery := compileWithTimeSpine(t, q, dialect)
			for _, fragment := range []string{"__domain", "__grid", "CROSS JOIN", "region", "COALESCE"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s dense dimension SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
		})
	}
}

func TestOrdinaryMetricDoesNotLowerDenseCalendar(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
	plan, sqlQuery := compileWithTimeSpine(t, q, "DORIS")
	if plan.DenseCalendar != nil {
		t.Fatalf("ordinary metric unexpectedly activated dense calendar: %#v", plan.DenseCalendar)
	}
	if strings.Contains(sqlQuery.SQL, "__calendar") || strings.Contains(sqlQuery.SQL, "analytics.calendar") {
		t.Fatalf("ordinary metric unexpectedly lowered a calendar stage:\n%s", sqlQuery.SQL)
	}
}

func compileWithTimeSpine(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SqlRenderResult) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	doc.SemanticModel[0].CustomExtensions = append(doc.SemanticModel[0].CustomExtensions, ossie.CustomExtension{
		VendorName: ossie.MetisExtensionVendor,
		Data:       `{"kind":"time_spine","dataset":"calendar","time_dimension":"day","grains":["day","week","month","quarter","year"]}`,
	})
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
