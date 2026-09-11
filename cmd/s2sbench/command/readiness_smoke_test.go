package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
)

func TestGeneratedFoundationSupportsMetisWithoutOKF(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0o700); err != nil {
		t.Fatal(err)
	}
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion,
		Engine:        "duckdb",
		EngineTitle:   "DuckDB",
		Database:      "tpcds",
		Schema:        "public",
		Generator:     "test",
		GeneratedAt:   "2026-09-03T00:00:00Z",
		Tables: []readiness.CatalogTable{{
			Name: "store_sales", RowCount: 1,
			Columns: []readiness.CatalogColumn{{Position: 1, Name: "ss_ext_sales_price", Type: "DECIMAL"}},
		}},
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog", "catalog.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	projection, err := loadGeneratedProjection(root, workload.Bundle{}, readiness.ArmMetisMCP)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Ledger.ProjectionVersion != readiness.CatalogProjectionVersion || len(projection.Ledger.Facts) == 0 {
		t.Fatalf("foundation projection = %#v", projection.Ledger)
	}

	_, err = loadGeneratedProjection(root, workload.Bundle{}, readiness.ArmOKF)
	if err == nil || !strings.Contains(err.Error(), "no frozen OKF knowledge") {
		t.Fatalf("OKF foundation error = %v", err)
	}
}
