package compiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestSemiAdditiveTypedOuterRollupDialectLowering(t *testing.T) {
	query := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}}
	for _, tc := range []struct {
		name   string
		rollup string
		want   string
	}{
		{name: "sum", rollup: "sum", want: "SUM("},
		{name: "min", rollup: "min", want: "MIN("},
		{name: "max", rollup: "max", want: "MAX("},
	} {
		for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
			t.Run(tc.name+"/"+dialect, func(t *testing.T) {
				sqlQuery := compileSemiAdditiveTypedRollup(t, query, dialect, tc.rollup)
				if !strings.Contains(sqlQuery.SQL, "__metis_window_group_selected") || !strings.Contains(sqlQuery.SQL, "warehouse") {
					t.Fatalf("%s SQL does not preserve per-window-group selection:\n%s", dialect, sqlQuery.SQL)
				}
				if !strings.Contains(strings.ToUpper(sqlQuery.SQL), tc.want) {
					t.Fatalf("%s SQL missing typed %s outer rollup:\n%s", dialect, tc.rollup, sqlQuery.SQL)
				}
			})
		}
	}
}

func compileSemiAdditiveTypedRollup(t *testing.T, query query.SemanticQuery, dialect, rollup string) sql.SQLRenderResult {
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
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"],"rollup_aggregation":"` + rollup + `"}`
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
	return sqlQuery
}
