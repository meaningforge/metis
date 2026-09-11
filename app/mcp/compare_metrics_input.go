package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	service "github.com/meaningforge/metis/app/service/semantic"
)

func compareMetricsInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[service.MetricComparisonQuery](&jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("build compare_metrics MCP input schema: %v", err))
	}
	return schema
}
