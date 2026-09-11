package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
)

func main() {
	output := flag.String("output", "", "directory that receives the deterministic OKF bundle")
	flag.Parse()
	if strings.TrimSpace(*output) == "" {
		fmt.Fprintln(os.Stderr, "--output is required")
		os.Exit(2)
	}
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion,
		Engine:        "duckdb",
		EngineTitle:   "DuckDB",
		Database:      "s2sbench",
		Schema:        "analytics",
		Generator:     "s2sbench-format-check",
		GeneratedAt:   "2026-09-07T00:00:00Z",
		Tables: []readiness.CatalogTable{{
			Name: "orders", RowCount: 1,
			Columns: []readiness.CatalogColumn{{Position: 1, Name: "order_id", Type: "BIGINT"}},
		}},
	}
	projection, err := readiness.BuildCatalogProjection(catalog)
	if err != nil {
		fatal(err)
	}
	if err := projection.WriteBundle(*output); err != nil {
		fatal(err)
	}
	fmt.Printf("project=%s files=%d facts=%d projection=%s digest=%s\n", projection.Project, len(projection.Files), len(projection.Ledger.Facts), projection.Ledger.ProjectionVersion, projection.Ledger.Digest)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
