//go:build duckdb

// Package duckdb provides the executable DuckDB Backend integration.
package duckdb

import (
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/renderer/duckdb"
)

// New returns the one complete DuckDB Backend binding for explicit executable
// composition.
func New() backend.Backend {
	return backend.Backend{
		Type:          datasource.Type("duckdb"),
		Renderer:      duckdb.New(),
		DriverFactory: NewDriverFactory(),
	}
}
