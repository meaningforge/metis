package planner_test

import (
	"testing"

	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
)

func mustRenderer(t *testing.T, dialect string) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}
