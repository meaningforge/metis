package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	service "github.com/meaningforge/metis/app/service/semantic"
)

func dimensionValuesInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[service.DimensionValuesQuery](&jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("build get_dimension_values MCP input schema: %v", err))
	}
	return schema
}
