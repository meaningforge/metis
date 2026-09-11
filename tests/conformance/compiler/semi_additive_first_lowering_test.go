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

func TestSemiAdditiveFirstDialectLowering(t *testing.T) {
	q := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}}

	_, duckdb := compileSemiAdditiveFirst(t, q, "DUCKDB")
	if strings.Contains(strings.ToUpper(duckdb.SQL), "MIN_BY(") || strings.Contains(strings.ToLower(duckdb.SQL), "argmin(") {
		t.Fatalf("DuckDB SQL must use structural earliest-key lowering:\n%s", duckdb.SQL)
	}
	for _, fragment := range []string{"MIN(", "__metis_ordered_key", "JOIN", "snapshot_date", "inventory_quantity"} {
		if !strings.Contains(duckdb.SQL, fragment) {
			t.Fatalf("DuckDB SQL missing %q:\n%s", fragment, duckdb.SQL)
		}
	}

	_, doris := compileSemiAdditiveFirst(t, q, "DORIS")
	if !strings.Contains(strings.ToUpper(doris.SQL), "MIN_BY(") {
		t.Fatalf("Doris SQL must use MIN_BY earliest-value lowering:\n%s", doris.SQL)
	}

	_, clickhouse := compileSemiAdditiveFirst(t, q, "CLICKHOUSE")
	if !strings.Contains(strings.ToLower(clickhouse.SQL), "argmin(") {
		t.Fatalf("ClickHouse SQL must use argMin earliest-value lowering:\n%s", clickhouse.SQL)
	}
}

func compileSemiAdditiveFirst(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SQLQuery) {
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
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"first"}`
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
