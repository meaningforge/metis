package readiness

import (
	"bytes"
	"reflect"
	"testing"
)

func TestCatalogProjectionUsesPhysicalSchemaAndOfficialOKFShape(t *testing.T) {
	catalog := SourceCatalog{
		SchemaVersion: CatalogSchemaVersion,
		Engine:        "duckdb",
		EngineTitle:   "DuckDB",
		Database:      "tpcds",
		Schema:        "public",
		Specification: "TPC-DS 2.10.0",
		Generator:     "DuckDB tpcds dsdgen",
		GeneratedAt:   "2026-09-02T00:00:00Z",
		Tags:          []string{"tpc-ds"},
		Tables: []CatalogTable{
			{Name: "store_sales", RowCount: 10, Columns: []CatalogColumn{{Position: 1, Name: "ss_item_sk", Type: "BIGINT", Nullable: true}, {Position: 2, Name: "ss_ext_sales_price", Type: "DECIMAL(7,2)", Nullable: true}}},
			{Name: "item", RowCount: 3, Columns: []CatalogColumn{{Position: 1, Name: "i_item_sk", Type: "BIGINT", Nullable: true}}},
		},
	}
	first, err := BuildCatalogProjection(catalog)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildCatalogProjection(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if first.Ledger.Digest != second.Ledger.Digest || !reflect.DeepEqual(first.Files, second.Files) {
		t.Fatal("catalog projection is not deterministic")
	}
	for _, path := range []string{"index.md", "datasets/index.md", "datasets/tpcds.md", "tables/index.md", "tables/item.md", "tables/store_sales.md"} {
		if _, ok := first.Files[path]; !ok {
			t.Errorf("missing OKF file %q", path)
		}
	}
	storeSales := first.Files["tables/store_sales.md"]
	for _, want := range []string{"type: \"DuckDB Table\"", "# Schema", "| `ss_ext_sales_price` | `DECIMAL(7,2)` | yes |", "Row count at generation: `10`"} {
		if !bytes.Contains(storeSales, []byte(want)) {
			t.Errorf("store_sales OKF concept omits %q", want)
		}
	}
	if bytes.Contains(storeSales, []byte("total_sales")) || bytes.Contains(storeSales, []byte("ossie://")) {
		t.Fatal("catalog projection leaks Ossie semantic material")
	}
	if first.Ledger.ProjectionVersion != CatalogProjectionVersion || first.Ledger.Facts[0].Semantic {
		t.Fatalf("catalog projection ledger = %#v", first.Ledger)
	}
}

func TestAddReferenceAgentConceptBodiesPreservesCatalogFacts(t *testing.T) {
	catalog := SourceCatalog{
		SchemaVersion: CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB", Database: "tpcds", Schema: "public",
		Generator: "test", GeneratedAt: "2026-09-03T00:00:00Z",
		Tables: []CatalogTable{{Name: "item", Columns: []CatalogColumn{{Position: 1, Name: "i_item_sk", Type: "BIGINT"}}}},
	}
	projection, err := BuildCatalogProjection(catalog)
	if err != nil {
		t.Fatal(err)
	}
	digest := projection.Ledger.Digest
	if err := ApplyReferenceAgentConceptDocuments(projection, map[string]ReferenceAgentConceptDocument{
		"datasets/tpcds.md": {Frontmatter: map[string]any{"type": "DuckDB Dataset", "title": "TPC-DS retail data"}, Body: "Use the catalog concepts as factual authority."},
		"tables/item.md":    {Frontmatter: map[string]any{"type": "DuckDB Table", "title": "Retail items"}, Body: "The i_item_sk field is available for inspection.\n\n# Common query patterns\n\n```sql\nSELECT i_item_sk FROM item;\n```"},
	}); err != nil {
		t.Fatal(err)
	}
	if projection.Ledger.Digest != digest {
		t.Fatal("editorial guidance changed the catalog fact ledger")
	}
	if !bytes.Contains(projection.Files["tables/item.md"], []byte("# Reference-agent guidance")) {
		t.Fatal("reference-agent guidance was not added to the concept")
	}
	if !bytes.Contains(projection.Files["tables/item.md"], []byte("title: Retail items")) || !bytes.Contains(projection.Files["tables/item.md"], []byte("source-catalog")) {
		t.Fatal("reference-agent editorial metadata did not merge while source provenance stayed fixed")
	}
}
