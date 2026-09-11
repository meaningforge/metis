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

func TestSemiAdditiveNullSkipDialectLowering(t *testing.T) {
	query := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			sqlQuery := compileSemiAdditiveNullSkip(t, query, dialect, "last", true)
			for _, fragment := range []string{"__metis_non_null_candidates", "inventory_quantity", "IS NOT NULL"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s null-skip SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			if dialect == "CLICKHOUSE" && (!strings.Contains(sqlQuery.SQL, "argMax(") || !strings.Contains(sqlQuery.SQL, "tuple(")) {
				t.Fatalf("ClickHouse deterministic null-skip must retain argMax tuple ordering:\n%s", sqlQuery.SQL)
			}
		})
	}

	first := compileSemiAdditiveNullSkip(t, query, "CLICKHOUSE", "first", false)
	if !strings.Contains(first.SQL, "argMin(") || !strings.Contains(first.SQL, "IS NOT NULL") {
		t.Fatalf("ClickHouse first null-skip lowering is incomplete:\n%s", first.SQL)
	}
}

func compileSemiAdditiveNullSkip(t *testing.T, query query.SemanticQuery, dialect, aggregation string, withTieBreak bool) sql.SQLRenderResult {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	if withTieBreak {
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
			tieBreak := ""
			if withTieBreak {
				tieBreak = `,"tie_break_dimension":"snapshot_sequence"`
			}
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"` + aggregation + `"` + tieBreak + `,"null_policy":"skip"}`
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
