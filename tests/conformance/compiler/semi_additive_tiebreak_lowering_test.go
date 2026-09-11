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

func TestSemiAdditiveTieBreakDialectLowering(t *testing.T) {
	query := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}}

	_, duckdb := compileSemiAdditiveTieBreak(t, query, "DUCKDB", "first")
	for _, fragment := range []string{"__metis_ordered_primary", "__metis_ordered_secondary", "snapshot_date", "snapshot_sequence", "MIN("} {
		if !strings.Contains(duckdb.SQL, fragment) {
			t.Fatalf("DuckDB SQL missing %q:\n%s", fragment, duckdb.SQL)
		}
	}

	_, doris := compileSemiAdditiveTieBreak(t, query, "DORIS", "last")
	for _, fragment := range []string{"__metis_ordered_primary", "__metis_ordered_secondary", "snapshot_date", "snapshot_sequence", "MAX("} {
		if !strings.Contains(doris.SQL, fragment) {
			t.Fatalf("Doris SQL missing %q:\n%s", fragment, doris.SQL)
		}
	}
	if strings.Contains(strings.ToUpper(doris.SQL), "MAX_BY(") {
		t.Fatalf("Doris deterministic tie-break must use structural two-key selection, not MAX_BY composite assumptions:\n%s", doris.SQL)
	}

	_, clickhouse := compileSemiAdditiveTieBreak(t, query, "CLICKHOUSE", "first")
	if !strings.Contains(clickhouse.SQL, "argMin(") || !strings.Contains(clickhouse.SQL, "tuple(") || !strings.Contains(clickhouse.SQL, "snapshot_sequence") {
		t.Fatalf("ClickHouse deterministic first must use argMin over tuple(time, tie-break):\n%s", clickhouse.SQL)
	}
}

func TestSemiAdditiveWindowGroupingSelectsPerGroupThenRollsUp(t *testing.T) {
	query := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			_, sqlQuery := compileSemiAdditiveWindowGrouping(t, query, dialect)
			for _, fragment := range []string{"__metis_window_group_selected", "warehouse", "snapshot_date", "SUM("} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			switch dialect {
			case "DORIS":
				if !strings.Contains(strings.ToUpper(sqlQuery.SQL), "MAX_BY(") {
					t.Fatalf("Doris window grouping must select latest snapshot per warehouse:\n%s", sqlQuery.SQL)
				}
			case "CLICKHOUSE":
				if !strings.Contains(sqlQuery.SQL, "argMax(") {
					t.Fatalf("ClickHouse window grouping must select latest snapshot per warehouse:\n%s", sqlQuery.SQL)
				}
			case "DUCKDB":
				if !strings.Contains(sqlQuery.SQL, "__metis_ordered_key") || !strings.Contains(sqlQuery.SQL, "MAX(") {
					t.Fatalf("DuckDB window grouping must use structural latest-key selection:\n%s", sqlQuery.SQL)
				}
			}
		})
	}
}

func TestSemiAdditiveQueriedTimeWindowUsesRawOrderingKey(t *testing.T) {
	week := query.TimeGrainWeek
	query := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "snapshot_date", Grain: &week}}}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			_, sqlQuery, err := compile(t, query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range []string{"snapshot_date", "__metis_ordered_snapshot_date"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			switch dialect {
			case "DORIS":
				if !strings.Contains(strings.ToUpper(sqlQuery.SQL), "MAX_BY(") {
					t.Fatalf("Doris queried time window must use latest raw snapshot key:\n%s", sqlQuery.SQL)
				}
			case "CLICKHOUSE":
				if !strings.Contains(sqlQuery.SQL, "argMax(") || !strings.Contains(sqlQuery.SQL, "toStartOfWeek") {
					t.Fatalf("ClickHouse queried time window must bucket by week and order by raw snapshot:\n%s", sqlQuery.SQL)
				}
			case "DUCKDB":
				if !strings.Contains(sqlQuery.SQL, "__metis_ordered_key") || !strings.Contains(sqlQuery.SQL, "MAX(") {
					t.Fatalf("DuckDB queried time window must use structural latest raw-key selection:\n%s", sqlQuery.SQL)
				}
			}
		})
	}
}

func compileSemiAdditiveTieBreak(t *testing.T, query query.SemanticQuery, dialect, aggregation string) (*semanticplan.SemanticPlan, sql.SQLRenderResult) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	for i := range model.Datasets {
		if model.Datasets[i].Name != "inventory_snapshot" {
			continue
		}
		model.Datasets[i].Fields = append(model.Datasets[i].Fields, ossie.Field{
			Name: "snapshot_sequence", Datatype: ossie.DataTypeInteger,
			Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "inventory_snapshot.snapshot_sequence"}}},
			Dimension:  &ossie.Dimension{},
		})
	}
	found := false
	for i := range model.Metrics {
		if model.Metrics[i].Name != "inventory_balance" {
			continue
		}
		for j := range model.Metrics[i].CustomExtensions {
			ext := &model.Metrics[i].CustomExtensions[j]
			if ext.VendorName != ossie.MetisExtensionVendor {
				continue
			}
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"` + aggregation + `","tie_break_dimension":"snapshot_sequence"}`
			found = true
			break
		}
	}
	if !found {
		t.Fatal("inventory_balance semi-additive extension not found")
	}
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

func compileSemiAdditiveWindowGrouping(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SQLRenderResult) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	found := false
	for i := range model.Metrics {
		if model.Metrics[i].Name != "inventory_balance" {
			continue
		}
		for j := range model.Metrics[i].CustomExtensions {
			ext := &model.Metrics[i].CustomExtensions[j]
			if ext.VendorName != ossie.MetisExtensionVendor {
				continue
			}
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"]}`
			found = true
			break
		}
	}
	if !found {
		t.Fatal("inventory_balance semi-additive extension not found")
	}
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
