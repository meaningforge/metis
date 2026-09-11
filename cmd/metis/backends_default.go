//go:build !duckdb

package main

import (
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/backend/clickhouse"
	"github.com/meaningforge/metis/execution/backend/doris"
)

func defaultBackendBindings() []backend.Backend {
	return []backend.Backend{clickhouse.New(), doris.New()}
}
