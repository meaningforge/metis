//go:build duckdb

package main

import "testing"

func TestDefaultBackendsRegistersExecutableDuckDBBackend(t *testing.T) {
	backends, err := defaultBackends()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := backends.Resolve("duckdb")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Renderer == nil || binding.DriverFactory == nil || binding.SQLDialect() != "DUCKDB" {
		t.Fatalf("DuckDB Backend = %#v", binding)
	}
}
