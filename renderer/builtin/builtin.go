// Package builtin explicitly composes the Renderers shipped with Metis. It
// does not implement a selectable default or ANSI Renderer.
package builtin

import (
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/clickhouse"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/renderer/duckdb"
)

// Renderers returns fresh instances of the Renderers shipped with Metis.
// Callers construct the sole registry through renderer.NewRegistry.
func Renderers() []renderer.Renderer {
	return []renderer.Renderer{duckdb.New(), doris.New(), clickhouse.New()}
}
