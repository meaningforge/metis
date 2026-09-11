package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
)

// compileInputSchema exposes the dialects registered for compile-only output
// so MCP clients can select a valid physical SQL language without guessing.
func compileInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[service.AgentCompileRequest](&jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("build compile MCP input schema: %v", err))
	}

	dialect, ok := schema.Properties["dialect"]
	if !ok {
		panic("build compile MCP input schema: dialect property is missing")
	}
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		panic(fmt.Sprintf("create compile MCP Renderer registry: %v", err))
	}
	for _, supported := range registry.Dialects() {
		dialect.Enum = append(dialect.Enum, string(supported))
	}
	return schema
}
