package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListMetricsMCPInputSchemaUsesTypedMetaFilter(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(service.NewDiscoveryService(nil), nil, nil, nil).Connect(ctx, serverTransport, nil)
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
	var listMetrics *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == ToolListMetrics {
			listMetrics = tool
			break
		}
	}
	if listMetrics == nil {
		t.Fatal("list_metrics tool is not registered")
	}

	encoded, err := json.Marshal(listMetrics.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("list_metrics input schema has no object properties: %s", encoded)
	}
	if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
		t.Fatalf("list_metrics input schema permits unknown top-level properties: %s", encoded)
	}
	search, ok := properties["search"]
	if !ok {
		t.Fatalf("list_metrics input schema has no search property: %s", encoded)
	}
	searchEncoded, err := json.Marshal(search)
	if err != nil {
		t.Fatal(err)
	}
	searchSchema := string(searchEncoded)
	if !strings.Contains(searchSchema, `"anyOf"`) || !strings.Contains(searchSchema, `"type":"string"`) || !strings.Contains(searchSchema, `"type":"array"`) {
		t.Fatalf("list_metrics search schema is not scalar-or-list: %s", searchEncoded)
	}

	metaFilter, ok := properties["meta_filter"]
	if !ok {
		t.Fatalf("list_metrics input schema has no meta_filter property: %s", encoded)
	}
	if _, ok := properties["models"]; ok {
		t.Fatalf("list_metrics input schema leaked legacy top-level models: %s", encoded)
	}
	if _, ok := properties["cursor"]; !ok {
		t.Fatalf("list_metrics input schema has no cursor property: %s", encoded)
	}
	metaEncoded, err := json.Marshal(metaFilter)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaEncoded), `"models"`) {
		t.Fatalf("list_metrics meta_filter schema has no typed models field: %s", metaEncoded)
	}
	if !strings.Contains(string(metaEncoded), `"additionalProperties":false`) {
		t.Fatalf("list_metrics meta_filter permits unknown properties: %s", metaEncoded)
	}
}
