package mcp

import (
	"github.com/google/jsonschema-go/jsonschema"
	attributionanalytics "github.com/meaningforge/metis/analytics/attribution"
)

// attributeMetricOutputSchema describes the structured result validated by
// the MCP SDK. Attribution member values are validated as JSON scalars by
// analytics/attribution before they cross the transport boundary.
func attributeMetricOutputSchema() *jsonschema.Schema {
	return analyticalOutputSchema[attributionanalytics.AttributeMetricResult]()
}
