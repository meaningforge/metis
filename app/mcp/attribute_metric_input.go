package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	service "github.com/meaningforge/metis/app/service/semantic"
)

func attributeMetricInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[service.MetricAttributionQuery](&jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("build attribute_metric MCP input schema: %v", err))
	}
	return schema
}
