//go:build duckdb

package duckdb_test

import (
	"path/filepath"
	"testing"
)

func databasePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "metis-conformance.duckdb")
}
