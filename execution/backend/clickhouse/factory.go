package clickhouse

import (
	"context"
	"fmt"

	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

// DriverFactory executes ClickHouse SQL through the official database/sql
// driver over HTTP. It owns only ClickHouse connection configuration.
type DriverFactory struct{}

func NewDriverFactory() *DriverFactory { return &DriverFactory{} }

func (*DriverFactory) DataSourceType() datasource.Type { return "clickhouse" }

func (*DriverFactory) ValidateConfig(config map[string]string) error { return validateConfig(config) }

func (f *DriverFactory) OpenDataSource(ctx context.Context, request driver.OpenRequest) (driver.Runtime, error) {
	if f == nil {
		return nil, fmt.Errorf("ClickHouse Driver Factory is required")
	}
	if err := f.ValidateConfig(request.Config); err != nil {
		return nil, err
	}
	options, err := clickHouseOptions(request.Config, request.Secrets)
	if err != nil {
		return nil, err
	}
	db := clickhousedriver.OpenDB(options)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return newDataSourceRuntime(db), nil
}
