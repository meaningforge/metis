package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiscoveryToolInputSchemasCarryParameterGuidance(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), nil, nil).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]map[string]string{
		ToolListProjects:     {"project_ids": "exact project IDs"},
		ToolListModels:       {"search": "contained dataset, metric, and dimension names", "cursor": "next_cursor"},
		ToolListMetrics:      {"search": "governed aliases, typed semantic constraints", "cursor": "next_cursor"},
		ToolGetDimensions:    {"model": "metric-free dimension discovery", "metrics": "compatible with every selected metric", "search": "after metric compatibility", "cursor": "next_cursor"},
		ToolGetRelationships: {"search": "temporal evidence"},
	}
	seen := make(map[string]bool, len(wants))
	for _, tool := range result.Tools {
		properties, ok := wants[tool.Name]
		if !ok {
			continue
		}
		seen[tool.Name] = true
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		for property, want := range properties {
			if !strings.Contains(schema.Properties[property].Description, want) {
				t.Fatalf("%s.%s description = %q, want guidance %q", tool.Name, property, schema.Properties[property].Description, want)
			}
		}
	}
	for tool := range wants {
		if !seen[tool] {
			t.Fatalf("tool %q is not registered", tool)
		}
	}
}
