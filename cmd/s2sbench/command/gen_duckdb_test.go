//go:build duckdb

package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/ossie"
)

func TestGenerateStandardTPCDSUsesFullSchemaAndExportsStandardFormats(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database := filepath.Join(root, "tpcds.duckdb")
	version, tables, err := generateStandardTPCDS(ctx, database, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(version, "tpcds-kit=2.10.0") || len(tables) != 24 {
		t.Fatalf("generator version=%q tables=%d, want recorded extension and 24 tables", version, len(tables))
	}
	for _, dir := range []string{"data/csv", "data/parquet"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := exportTPCDSData(ctx, root, database, tables)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 48 {
		t.Fatalf("exported assets = %d, want 48", len(assets))
	}
	db, err := openDuckDB(database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM public.store_sales").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Fatal("standard store_sales is empty")
	}
	catalog, err := readDuckDBCatalog(ctx, database, "tpcds", "public", tPCDSSpecification, version, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tables) != 24 {
		t.Fatalf("catalog tables = %d, want 24", len(catalog.Tables))
	}
	projection, err := readiness.BuildCatalogProjection(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := projection.Files["tables/store_sales.md"]; !ok {
		t.Fatal("catalog-derived OKF omits store_sales table")
	}
}

func TestValidateTPCDSOssieBindingsRejectsMissingPhysicalColumn(t *testing.T) {
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB",
		Database: "tpcds", Schema: "public", Generator: "test", GeneratedAt: "2026-09-02T00:00:00Z",
		Tables: []readiness.CatalogTable{{Name: "store_sales", Columns: []readiness.CatalogColumn{{Position: 1, Name: "ss_item_sk", Type: "BIGINT"}}}},
	}
	model := []byte(`version: "0.2.0.dev0"
semantic_model:
  - name: tpcds_retail_model
    datasets:
      - name: store_sales
        source: tpcds.public.store_sales
        primary_key: [ss_item_sk]
        fields:
          - name: item
            expression:
              dialects:
                - dialect: ANSI_SQL
                  expression: ss_item_sk
    metrics:
      - name: total_sales
        expression:
          dialects:
            - dialect: ANSI_SQL
              expression: SUM(store_sales.ss_item_sk)
`)
	if err := validateTPCDSOssieBindings(model, catalog); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	invalid := []byte(strings.Replace(string(model), "store_sales.ss_item_sk", "store_sales.ss_missing", 1))
	if err := validateTPCDSOssieBindings(invalid, catalog); err == nil || !strings.Contains(err.Error(), "ss_missing") {
		t.Fatalf("missing metric column error = %v", err)
	}
}

func TestBuildTPCDSOssieModelUsesCatalogAsDatasetAuthority(t *testing.T) {
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB",
		Database: "tpcds", Schema: "public", Generator: "test", GeneratedAt: "2026-09-02T00:00:00Z",
		Tables: []readiness.CatalogTable{
			{Name: "date_dim", Columns: []readiness.CatalogColumn{{Position: 1, Name: "d_date_sk", Type: "BIGINT"}}},
			{Name: "store_sales", Columns: []readiness.CatalogColumn{{Position: 1, Name: "ss_item_sk", Type: "BIGINT"}}},
		},
	}
	template := []byte(`version: "0.2.0.dev0"
semantic_model:
  - name: tpcds_retail_model
    datasets:
      - name: date_dim
        source: tpcds.public.date_dim
        fields:
          - name: d_date_sk
            expression:
              dialects: [{dialect: ANSI_SQL, expression: d_date_sk}]
          - name: d_month_name
            expression:
              dialects: [{dialect: ANSI_SQL, expression: d_month_name}]
`)
	generated, err := buildTPCDSOssieModel(template, catalog)
	if err != nil {
		t.Fatal(err)
	}
	document, err := ossie.NewLoader().Load(generated)
	if err != nil {
		t.Fatal(err)
	}
	datasets := document.SemanticModel[0].Datasets
	if len(datasets) != 2 || datasets[0].Name != "date_dim" || datasets[1].Name != "store_sales" {
		t.Fatalf("generated datasets = %#v", datasets)
	}
	if len(datasets[0].Fields) != 1 || datasets[0].Fields[0].Name != "d_date_sk" {
		t.Fatalf("unbound template field survived: %#v", datasets[0].Fields)
	}
}

func TestTPCDSCaseDefinitionsHaveIndependentReferenceSQL(t *testing.T) {
	for _, definition := range tpcdsCaseDefinitions() {
		if !strings.Contains(strings.ToUpper(definition.referenceSQL), "SELECT") || definition.query.Model == "" {
			t.Fatalf("incomplete definition %#v", definition)
		}
	}
}

func TestSalesByBrandUsesGenericSalesMetricAndExplicitGrouping(t *testing.T) {
	for _, definition := range tpcdsCaseDefinitions() {
		if definition.name != "sales_by_brand" {
			continue
		}
		if len(definition.query.Metrics) != 1 || definition.query.Metrics[0].Name != "total_sales" {
			t.Fatalf("sales_by_brand metrics = %#v, want total_sales", definition.query.Metrics)
		}
		if len(definition.query.Dimensions) != 1 || definition.query.Dimensions[0].Name != "i_brand" {
			t.Fatalf("sales_by_brand dimensions = %#v, want i_brand", definition.query.Dimensions)
		}
		for _, required := range []string{"For each product brand", "alphabetically by brand", "first 100 rows"} {
			if !strings.Contains(definition.question, required) {
				t.Fatalf("sales_by_brand question %q does not make %q explicit", definition.question, required)
			}
		}
		if !strings.Contains(definition.referenceSQL, "AS total_sales") || strings.Contains(definition.referenceSQL, "AS sales_by_brand") {
			t.Fatalf("sales_by_brand reference SQL has ambiguous metric identity: %s", definition.referenceSQL)
		}
		return
	}
	t.Fatal("sales_by_brand case is missing")
}

func TestGenCommandDefaultsOutputToDotWorkload(t *testing.T) {
	command := newGenCommand(&strings.Builder{})
	flag := command.Flags().Lookup("output")
	if flag == nil || flag.DefValue != ".workload" {
		t.Fatalf("output default = %#v, want .workload", flag)
	}
}

func TestCanonicalizeResultRows(t *testing.T) {
	rows := []scenarios.ResultRow{
		{{ValueKind: scenarios.ResultString, Canonical: "z"}},
		{{ValueKind: scenarios.ResultString, Canonical: "a"}},
	}
	canonicalizeResultRows(rows)
	if rows[0][0].Canonical != "a" || rows[1][0].Canonical != "z" {
		t.Fatalf("canonical rows = %#v", rows)
	}
}
