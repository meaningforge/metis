package mcp

import (
	"context"
	"encoding/json"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Decode original filter bytes before the SDK constructs typed tool input so
// every MCP path enforces the shared string-literal operand contract.
func guardFilterOperands(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method == "tools/call" {
			if params, ok := request.GetParams().(*mcp.CallToolParamsRaw); ok {
				var err error
				switch params.Name {
				case ToolCompile, ToolQueryMetrics:
					var input struct {
						Filters query.Predicate `json:"filters"`
					}
					err = json.Unmarshal(params.Arguments, &input)
				case ToolAttributeMetric, ToolCompareMetrics:
					// These workflows deliberately retain their flat conjunction
					// contract in RFC-0091, but still require the shared operand guard.
					var input struct {
						Filters []query.Filter `json:"filters"`
					}
					err = json.Unmarshal(params.Arguments, &input)
				}
				if err != nil {
					encoded := encodeToolError(&serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid filter operand", Details: map[string]any{"cause": err.Error()}})
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: encoded.Error()}}}, nil
				}
			}
		}
		return next(ctx, method, request)
	}
}
