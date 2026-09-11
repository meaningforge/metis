package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	service "github.com/meaningforge/metis/app/service/semantic"
)

// queryMetricsInputSchema exposes only semantic intent. Physical placement is
// intentionally absent because it is derived from the resolved project.
func queryMetricsInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[service.AgentQueryMetricsRequest](&jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("build query_metrics MCP input schema: %v", err))
	}
	schema.Required = append(schema.Required, "output_metrics")
	return schema
}
