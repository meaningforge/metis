package doris

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

// DriverFactory executes Doris SQL through its MySQL-compatible wire protocol.
// It owns only Doris connection configuration; semantic planning and Backend
// selection remain outside this package.
type DriverFactory struct{}

// NewDriverFactory creates a Doris Driver Factory for integrations that need
// to compose the driver independently of the default Backend binding.
func NewDriverFactory() *DriverFactory { return &DriverFactory{} }

func (*DriverFactory) DataSourceType() datasource.Type { return "doris" }

// ValidateConfig validates the closed v1 Doris connection shape.
// Credentials are always supplied through the Runner SecretResolver.
func (*DriverFactory) ValidateConfig(config map[string]string) error {
	return validateConfig(config)
}

func (f *DriverFactory) OpenDataSource(ctx context.Context, request driver.OpenRequest) (driver.Runtime, error) {
	if f == nil {
		return nil, fmt.Errorf("Doris Driver Factory is required")
	}
	if err := f.ValidateConfig(request.Config); err != nil {
		return nil, err
	}
	address, err := connectionAddress(request.Config, request.Secrets)
	if err != nil {
		return nil, err
	}
	config := mysql.NewConfig()
	config.Net = "tcp"
	config.Addr = address
	config.ParseTime = true
	config.Loc = time.UTC
	database, _, err := driver.ConfigValue(request.Config, request.Secrets, "database")
	if err != nil {
		return nil, err
	}
	if database != "" {
		config.DBName = database
	}
	username, _, err := driver.ConfigValue(request.Config, request.Secrets, "username")
	if err != nil {
		return nil, err
	}
	if username != "" {
		config.User = username
	}
	if _, exists := request.Config["password"]; exists {
		reference, refErr := passwordRef(request.Config)
		if refErr != nil {
			return nil, refErr
		}
		if request.Secrets == nil {
			return nil, fmt.Errorf("Doris password secret is unavailable")
		}
		password, found := request.Secrets.Value(reference)
		if !found || password == "" {
			return nil, fmt.Errorf("Doris password secret is unavailable")
		}
		config.Passwd = password
	}
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &dataSourceRuntime{db: db}, nil
}
