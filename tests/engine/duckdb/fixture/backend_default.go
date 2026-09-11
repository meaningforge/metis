//go:build !duckdb

package fixture

import (
	"context"
	"fmt"

	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Backend keeps S2SBench buildable in the default CGO-free flavor. Live
// embedded execution is intentionally available only with -tags duckdb.
type Backend struct{}

func New(string) (*Backend, error) {
	return nil, fmt.Errorf("embedded DuckDB fixture requires the duckdb build flavor")
}

func (*Backend) Close(context.Context) error { return nil }
func (*Backend) Prepare(context.Context, scenarios.Scenario) error {
	return fmt.Errorf("embedded DuckDB fixture requires the duckdb build flavor")
}
func (*Backend) PrepareFixture(context.Context, fixtures.ID) error {
	return fmt.Errorf("embedded DuckDB fixture requires the duckdb build flavor")
}
func (*Backend) Execute(context.Context, ...string) error {
	return fmt.Errorf("embedded DuckDB fixture requires the duckdb build flavor")
}
func (*Backend) RunSQL(context.Context, string, ...sql.QueryParameter) (scenarios.ResultSet, error) {
	return scenarios.ResultSet{}, fmt.Errorf("embedded DuckDB fixture requires the duckdb build flavor")
}
func (*Backend) PhysicalSchema(context.Context) (string, error) {
	return "", fmt.Errorf("embedded DuckDB fixture requires the duckdb build flavor")
}
