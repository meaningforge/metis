package mcp

import (
	"github.com/google/jsonschema-go/jsonschema"
	comparisonanalytics "github.com/meaningforge/metis/analytics/comparison"
)

// compareMetricsOutputSchema describes the structured result validated by the
// MCP SDK. Comparison member values are validated as JSON scalars by
// analytics/comparison before they cross the transport boundary.
func compareMetricsOutputSchema() *jsonschema.Schema {
	return analyticalOutputSchema[comparisonanalytics.Result]()
}
