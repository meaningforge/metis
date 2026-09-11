package agentquery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/auth"
	metismcp "github.com/meaningforge/metis/app/mcp"
	"github.com/meaningforge/metis/app/rest"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOntologyDiscoveryResolutionRESTMCPParity(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	// Use an existing String dimension; no synthetic physical query path.
	doc.Ontology = []map[string]any{{"concept": "Region", "type": "ValueType", "extends": []string{"String"}}}
	doc.Name = "Ontology parity fixture"
	var target string
	for _, m := range doc.SemanticModel {
		for _, ds := range m.Datasets {
			for _, f := range ds.Fields {
				if f.Dimension != nil && f.Datatype == ossie.DataTypeString {
					target = ds.Name + "." + f.Name
				}
			}
		}
	}
	if target == "" {
		t.Fatal("fixture needs String dimension")
	}
	doc.OntologyMappings = []map[string]any{{"name": "mapping", "concept_mappings": []map[string]any{{"concept": "Region", "object_mappings": []map[string]any{{"expression": target}}}}}}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	discovery := service.NewDiscoveryService(manifest.NewStore(snapshot))
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{TenantID: "tenant", SubjectID: "subject", Scopes: []string{auth.ScopeAll}})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rest.RegisterAgentSemanticRoutes(router.Group("/v1"), discovery)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := metismcp.NewServer(discovery, nil).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "ontology-test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	ref := ""
	for _, operation := range []string{"search", "resolve"} {
		input := map[string]any{"project_id": "finance"}
		tool := metismcp.ToolSearchOntologyConcepts
		if operation == "search" {
			input["query"] = "Region"
		} else {
			tool = metismcp.ToolResolveOntologyConcept
			input["concept_ref"] = ref
		}
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(http.MethodPost, "/v1/projects/finance/ontology/"+operation, bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("REST %d: %s", response.Code, response.Body.String())
		}
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: input})
		if err != nil || result.IsError {
			t.Fatalf("MCP %+v %v", result, err)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var restValue, mcpValue map[string]any
		if err = json.Unmarshal(response.Body.Bytes(), &restValue); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(encoded, &mcpValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(restValue, mcpValue) {
			t.Fatalf("REST=%v MCP=%v", restValue, mcpValue)
		}
		if operation == "search" {
			ref = restValue["concepts"].([]any)[0].(map[string]any)["concept_ref"].(string)
		} else if restValue["state"] != "unique" {
			t.Fatal(restValue)
		}
	}
}
