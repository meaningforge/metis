// Package clickhouse provides the executable ClickHouse Backend integration.
package clickhouse

import (
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	renderer "github.com/meaningforge/metis/renderer/clickhouse"
)

// New returns the one complete ClickHouse Backend binding for explicit
// executable composition.
func New() backend.Backend {
	return backend.Backend{
		Type:          datasource.Type("clickhouse"),
		Renderer:      renderer.New(),
		DriverFactory: NewDriverFactory(),
	}
}
