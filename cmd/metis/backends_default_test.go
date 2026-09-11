//go:build !duckdb

package main

import "testing"

func TestDefaultBackendsRejectsDuckDBWithoutExplicitBuildFlavor(t *testing.T) {
	backends, err := defaultBackends()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backends.Resolve("duckdb"); err == nil {
		t.Fatal("default build advertised an opt-in DuckDB Backend")
	}
}
