package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCompileMCPInputSchemaEnumeratesRegisteredDialects(t *testing.T) {
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
	var compile *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == ToolCompile {
			compile = tool
			break
		}
	}
	if compile == nil {
		t.Fatal("compile tool is not registered")
	}

	encoded, err := json.Marshal(compile.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}

	got := schema.Properties["dialect"].Enum
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]string, 0)
	for _, dialect := range registry.Dialects() {
		want = append(want, string(dialect))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compile dialect enum = %v, want %v", got, want)
	}
}

func TestQueryMetricsMCPInputSchemaContainsOnlySemanticIntent(t *testing.T) {
	encoded, err := json.Marshal(queryMetricsInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	if _, exists := schema.Properties["dialect"]; exists {
		t.Fatalf("query_metrics schema exposes dialect: %s", encoded)
	}
	if _, exists := schema.Properties["data_source"]; exists {
		t.Fatalf("query_metrics schema exposes data_source: %s", encoded)
	}
	if !containsString(schema.Required, "output_metrics") {
		t.Fatalf("query_metrics required = %v, want output_metrics", schema.Required)
	}
}

func TestQueryMetricsToolRegistersOnlyWhenServiceIsProvided(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithQueryMetrics(service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), service.NewQueryMetricsService(nil, nil, nil), nil, nil).Connect(ctx, serverTransport, nil)
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
	instructions := clientSession.InitializeResult().Instructions
	for _, guidance := range []string{"Use query_metrics when result rows are needed", "use compile_sql only when physical SQL is explicitly requested", "never reproduce it in handwritten SQL"} {
		if !strings.Contains(instructions, guidance) {
			t.Fatalf("query_metrics Agent guidance lacks %q: %s", guidance, instructions)
		}
	}
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == ToolQueryMetrics {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
				t.Fatalf("query_metrics annotations = %#v", tool.Annotations)
			}
			if !strings.Contains(tool.Description, "Use for result rows") || !strings.Contains(tool.Description, "semantic intent only") {
				t.Fatalf("query_metrics description = %q", tool.Description)
			}
			return
		}
	}
	t.Fatal("query_metrics tool is not registered")
}

func TestDimensionValuesSchemaAndToolExposeOnlyClosedGovernedIntent(t *testing.T) {
	encoded, err := json.Marshal(dimensionValuesInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	if !containsString(schema.Required, "dimension") {
		t.Fatalf("get_dimension_values required = %v, want dimension", schema.Required)
	}
	for _, allowed := range []string{"project_id", "dimension", "metrics", "grain", "limit"} {
		if _, ok := schema.Properties[allowed]; !ok {
			t.Fatalf("get_dimension_values schema omits %q: %s", allowed, encoded)
		}
	}
	for _, forbidden := range []string{"dialect", "data_source", "target", "renderer", "driver", "sql", "filters", "where", "order_by", "timeout", "max_rows"} {
		if _, ok := schema.Properties[forbidden]; ok {
			t.Fatalf("get_dimension_values schema exposes %q: %s", forbidden, encoded)
		}
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithDimensionValues(
		service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), nil,
		service.NewDimensionValuesService(nil, nil, nil, nil), nil, nil, nil, nil,
	).Connect(ctx, serverTransport, nil)
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
	instructions := clientSession.InitializeResult().Instructions
	for _, guidance := range []string{"Use get_dimension_values only", "filter wording may not match current stored members"} {
		if !strings.Contains(instructions, guidance) {
			t.Fatalf("get_dimension_values Agent guidance lacks %q: %s", guidance, instructions)
		}
	}
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != ToolGetDimensionValues {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Fatalf("get_dimension_values annotations = %#v", tool.Annotations)
		}
		for _, guidance := range []string{
			"live", "selected canonical dimension", "filter-member wording",
			"Do not use it for grouping, ordering, top-k, or limit discovery",
			"submit those directly through semantic query fields", "current warehouse evidence",
		} {
			if strings.Contains(tool.Description, guidance) {
				continue
			}
			t.Fatalf("get_dimension_values description = %q", tool.Description)
		}
		return
	}
	t.Fatal("get_dimension_values tool is not registered")
}

func TestAttributeMetricSchemaAndToolExposeOnlyGovernedIntent(t *testing.T) {
	encoded, err := json.Marshal(attributeMetricInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	for _, guidance := range []string{"RFC3339", "evaluated independently", "Typed semantic predicates"} {
		if !strings.Contains(string(encoded), guidance) {
			t.Fatalf("attribute_metric schema lacks %q: %s", guidance, encoded)
		}
	}
	for _, forbidden := range []string{"dialect", "data_source", "target", "renderer", "driver", "limit", "order_by", "top_k", "sql"} {
		if _, ok := schema.Properties[forbidden]; ok {
			t.Fatalf("attribute_metric schema exposes %q: %s", forbidden, encoded)
		}
	}
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithExecution(service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), nil, service.NewAttributeMetricService(nil, nil, nil, nil), nil, nil).Connect(ctx, serverTransport, nil)
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
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == ToolAttributeMetric {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
				t.Fatalf("attribute_metric annotations = %#v", tool.Annotations)
			}
			for _, guidance := range []string{"contribution evidence", "business causality", "contribution ranks"} {
				if !strings.Contains(tool.Description, guidance) {
					t.Fatalf("attribute_metric description lacks %q: %s", guidance, tool.Description)
				}
			}
			return
		}
	}
	t.Fatal("attribute_metric tool is not registered")
}

func TestCompareMetricsSchemaAndToolExposeOnlyGovernedIntent(t *testing.T) {
	encoded, err := json.Marshal(compareMetricsInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	for _, guidance := range []string{"RFC3339", "one shared comparison grain", "One through sixteen"} {
		if !strings.Contains(string(encoded), guidance) {
			t.Fatalf("compare_metrics schema lacks %q: %s", guidance, encoded)
		}
	}
	for _, required := range []string{"metrics", "time_dimension", "baseline", "current"} {
		if !containsString(schema.Required, required) {
			t.Fatalf("compare_metrics required = %v, missing %q", schema.Required, required)
		}
	}
	for _, forbidden := range []string{"dialect", "data_source", "target", "renderer", "driver", "limit", "order_by", "top_k", "sql", "timeout"} {
		if _, ok := schema.Properties[forbidden]; ok {
			t.Fatalf("compare_metrics schema exposes %q: %s", forbidden, encoded)
		}
	}
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithAnalytics(service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), nil, nil, service.NewCompareMetricsService(nil, nil, nil, nil), nil, nil).Connect(ctx, serverTransport, nil)
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
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == ToolCompareMetrics {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint || !strings.Contains(tool.Description, "absent rows") {
				t.Fatalf("compare_metrics contract = %#v", tool)
			}
			return
		}
	}
	t.Fatal("compare_metrics tool is not registered")
}

func TestCompareMetricsOutputSchemaAcceptsOnlyMemberScalars(t *testing.T) {
	schema := compareMetricsOutputSchema()
	value := schema.Properties["rows"].Items.Properties["members"].Items.Properties["value"]
	if got, want := value.Types, []string{"null", "string", "number", "boolean"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("member value types = %v, want %v", got, want)
	}
	if value.Items != nil || len(value.Properties) != 0 {
		t.Fatalf("member value schema permits a container: %#v", value)
	}
}

func TestAttributeMetricOutputSchemaAcceptsOnlyMemberScalars(t *testing.T) {
	schema := attributeMetricOutputSchema()
	dimension := schema.Properties["dimensions"].Items
	for _, strategy := range []string{"additive", "ratio"} {
		value := dimension.Properties[strategy].Properties["segments"].Items.Properties["value"]
		if got, want := value.Types, []string{"null", "string", "number", "boolean"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s member value types = %v, want %v", strategy, got, want)
		}
		if value.Items != nil || len(value.Properties) != 0 {
			t.Fatalf("%s member value schema permits a container: %#v", strategy, value)
		}
	}
}

func TestAnalyticalOutputSchemaFallsBackWithoutPanicking(t *testing.T) {
	type invalidResult struct {
		Unsupported chan int `json:"unsupported"`
	}

	schema := analyticalOutputSchema[invalidResult]()
	if got, want := schema.Types, []string{"object"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback schema types = %v, want %v", got, want)
	}
	if len(schema.Properties) != 0 {
		t.Fatalf("fallback schema properties = %#v, want permissive object", schema.Properties)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
