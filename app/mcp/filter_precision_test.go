package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPreservesExactFiltersBeforeServices(t *testing.T) {
	ctx := context.Background()
	a, b := mcp.NewInMemoryTransports()
	s, err := newServerWithQueryMetrics(service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), service.NewQueryMetricsService(nil, nil, nil), nil, nil).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := mcp.NewClient(&mcp.Implementation{Name: "precision", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, name := range []string{ToolCompile, ToolQueryMetrics} {
		dialect := ""
		if name == ToolCompile {
			dialect = `"dialect":"DUCKDB",`
		}
		for _, operand := range []string{"9007199254740993", "0.10000000000000000001", "[9007199254740992,9007199254740993]", "[0,1e-400]"} {
			args := json.RawMessage(`{"project_id":"demo",` + dialect + `"output_metrics":["revenue"],"filters":{"kind":"filter","filter":{"field":"amount","operator":"in","value":` + operand + `}}}`)
			result, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil || result == nil {
				t.Fatalf("%s exact input transport failure: %v", name, err)
			}
			encoded, _ := json.Marshal(result)
			message := string(encoded)
			if !strings.Contains(message, "PROJECT_ACCESS_DENIED") {
				t.Fatalf("expected authorization after exact decode: %s", message)
			}
			if strings.Contains(message, operand) {
				t.Fatal("response disclosed operand")
			}
		}
		args := json.RawMessage(`{"project_id":"demo",` + dialect + `"output_metrics":["revenue"],"filters":{"kind":"filter","filter":{"field":"amount","operator":"eq","value":1e9999}}}`)
		result, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		message := fmt.Sprint(err)
		if result != nil {
			encoded, _ := json.Marshal(result)
			message += string(encoded)
		}
		if !strings.Contains(message, "INVALID_QUERY") {
			t.Fatalf("%s accepted invalid numeric syntax: %s", name, message)
		}
		for _, operand := range []string{"0.1", "1.25", "9007199254740992", "[0.1,2]"} {
			args := json.RawMessage(`{"project_id":"demo",` + dialect + `"output_metrics":["revenue"],"filters":{"kind":"filter","filter":{"field":"amount","operator":"in","value":` + operand + `}}}`)
			result, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil || result == nil {
				t.Fatalf("compatible input rejected by decoder: %v", err)
			}
			encoded, _ := json.Marshal(result)
			// This deliberately unauthorized fixture proves the decoder admitted
			// valid numbers and continued to authorization, not query correctness.
			if !strings.Contains(string(encoded), "PROJECT_ACCESS_DENIED") {
				t.Fatalf("expected authorization after valid decode: %s", encoded)
			}
		}
	}
	for _, name := range []string{ToolAttributeMetric, ToolCompareMetrics} {
		const operand = "9007199254740993"
		args := json.RawMessage(`{"filters":[{"field":"amount","operator":"eq","value":` + operand + `}]}`)
		result, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		message := fmt.Sprint(err)
		if result != nil {
			encoded, _ := json.Marshal(result)
			message += string(encoded)
		}
		if strings.Contains(message, "filter number") || strings.Contains(message, "integer filter") || strings.Contains(message, operand) {
			t.Fatalf("%s exact response = %s", name, message)
		}
		invalid := json.RawMessage(`{"filters":[{"field":"amount","operator":"eq","value":1e9999}]}`)
		result, err = c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: invalid})
		message = fmt.Sprint(err)
		if result != nil {
			encoded, _ := json.Marshal(result)
			message += string(encoded)
		}
		if !strings.Contains(message, "INVALID_QUERY") {
			t.Fatalf("%s accepted invalid numeric syntax: %s", name, message)
		}
	}
}
