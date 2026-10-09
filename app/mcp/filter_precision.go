package mcp

import (
	"context"
	"encoding/json"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The SDK schema/defaulting path decodes untyped numbers before typed tool
// input. Validate original filter bytes first so rounding cannot hide a loss.
func guardFilterPrecision(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method == "tools/call" {
			if params, ok := request.GetParams().(*mcp.CallToolParamsRaw); ok {
				switch params.Name {
				case ToolCompile, ToolQueryMetrics, ToolAttributeMetric, ToolCompareMetrics:
					var input struct {
						Filters []query.Filter `json:"filters"`
					}
					if err := json.Unmarshal(params.Arguments, &input); err != nil {
						encoded := encodeToolError(&serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid filter operand", Details: map[string]any{"cause": err.Error()}})
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: encoded.Error()}}}, nil
					}
				}
			}
		}
		return next(ctx, method, request)
	}
}
