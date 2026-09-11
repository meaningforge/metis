// Package doris provides the executable Doris Backend integration.
package doris

import (
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/renderer/doris"
)

// New returns the one complete Doris Backend binding for explicit executable
// composition.
func New() backend.Backend {
	return backend.Backend{
		Type:          datasource.Type("doris"),
		Renderer:      doris.New(),
		DriverFactory: NewDriverFactory(),
	}
}
