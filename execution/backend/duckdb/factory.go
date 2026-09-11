//go:build duckdb

package duckdb

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

// DriverFactory opens embedded DuckDB databases in read-only mode. It owns
// only DuckDB connection configuration; admission and query limits remain in
// Runner.
type DriverFactory struct{}

// NewDriverFactory creates a DuckDB Driver Factory for integrations that need
// to compose the driver independently of the default Backend binding.
func NewDriverFactory() *DriverFactory { return &DriverFactory{} }

func (*DriverFactory) DataSourceType() datasource.Type { return "duckdb" }

// ValidateConfig validates the closed v1 DuckDB connection shape.
func (*DriverFactory) ValidateConfig(config map[string]string) error {
	return validateConfig(config)
}

func (f *DriverFactory) OpenDataSource(ctx context.Context, request driver.OpenRequest) (driver.Runtime, error) {
	if f == nil {
		return nil, fmt.Errorf("DuckDB Driver Factory is required")
	}
	if err := f.ValidateConfig(request.Config); err != nil {
		return nil, err
	}
	path, err := databasePath(request.Config, request.Secrets)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	connector, err := openConnector(ctx, path+"?access_mode=read_only")
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &dataSourceRuntime{db: db}, nil
}

type connectorResult struct {
	connector *duckdb.Connector
	err       error
}

// duckdb.NewConnector performs native file opening before database/sql can
// apply PingContext. Isolate that call so Runner's initialization deadline is
// still authoritative; a connector completing after cancellation closes
// itself instead of leaking a native database handle.
func openConnector(ctx context.Context, dsn string) (*duckdb.Connector, error) {
	results := make(chan connectorResult, 1)
	go func() {
		connector, err := duckdb.NewConnector(dsn, nil)
		if ctx.Err() != nil && connector != nil {
			_ = connector.Close()
			connector = nil
		}
		results <- connectorResult{connector: connector, err: err}
	}()
	select {
	case result := <-results:
		return result.connector, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
