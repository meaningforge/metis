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

func TestSemiAdditiveComposableIntermediateRetainsSelectorState(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			doc := semiAdditiveComposabilityDocument(t, false)
			sqlQuery, err := compileSemiAdditiveComposability(t, doc, dialect)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sqlQuery.SQL, "__metis_state_order_inventory_balance") {
				t.Fatalf("%s SQL does not retain selector order state:\n%s", dialect, sqlQuery.SQL)
			}
			if strings.Contains(sqlQuery.SQL, "inventory_balance`.`snapshot_date") || strings.Contains(sqlQuery.SQL, "inventory_balance.snapshot_date") {
				t.Fatalf("%s SQL re-reads discarded raw ordering state:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}

func TestSemiAdditiveTerminalScalarCompositionFailsClosed(t *testing.T) {
	doc := semiAdditiveComposabilityDocument(t, true)
	_, err := compileSemiAdditiveComposability(t, doc, "DORIS")
	if err == nil || !strings.Contains(err.Error(), "additive rollup boundary") {
		t.Fatalf("error = %v, want terminal scalar composability failure", err)
	}
}

func semiAdditiveComposabilityDocument(t *testing.T, terminalBase bool) *ossie.Document {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	for i := range model.Metrics {
		if model.Metrics[i].Name != "inventory_balance" {
			continue
		}
		for j := range model.Metrics[i].CustomExtensions {
			ext := &model.Metrics[i].CustomExtensions[j]
			if ext.VendorName != ossie.MetisExtensionVendor {
				continue
			}
			if terminalBase {
				ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"],"rollup_aggregation":"sum"}`
			}
			break
		}
		break
	}
	model.Metrics = append(model.Metrics, ossie.Metric{
		Name:     "inventory_balance_reselected",
		Datatype: ossie.DataTypeDecimal,
		Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{
			Dialect:    ossie.DialectANSISQL,
			Expression: "inventory_balance",
		}}},
		CustomExtensions: []ossie.CustomExtension{{
			VendorName: ossie.MetisExtensionVendor,
			Data:       `{"kind":"semi_additive","base_metric":"inventory_balance","non_additive_dimension":"snapshot_date","aggregation":"last"}`,
		}},
	})
	if err := ossie.ValidateDocument(doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func compileSemiAdditiveComposability(t *testing.T, doc *ossie.Document, dialect string) (sql.SqlStatement, error) {
	t.Helper()
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		return sql.SqlStatement{}, err
	}
	semanticQuery := query.SemanticQuery{
		Project:    projectName,
		Model:      modelName,
		Metrics:    []query.MetricRef{{Name: "inventory_balance_reselected"}},
		Dimensions: []query.DimensionRef{{Name: "warehouse"}},
	}
	renderer := mustRenderer(t, dialect)
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, renderer)
	if err != nil {
		return sql.SqlStatement{}, err
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		return sql.SqlStatement{}, err
	}
	sqlQuery, err := compilePlan(context.Background(), plan, renderer)
	if err != nil {
		return sql.SqlStatement{}, err
	}
	return sqlQuery, nil
}
