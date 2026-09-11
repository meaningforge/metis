package command

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/ossie"
)

func TestOKFGenUsesGoogleReferenceAgentSkillOrder(t *testing.T) {
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB", Database: "tpcds", Schema: "public",
		Generator: "test", GeneratedAt: "2026-09-03T00:00:00Z", Tables: []readiness.CatalogTable{{Name: "item", Columns: []readiness.CatalogColumn{{Position: 1, Name: "i_item_sk", Type: "BIGINT"}}}},
	}
	concepts := referenceAgentConcepts(catalog)
	prompt, err := referenceAgentPrompt(catalog, concepts[0], "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Generate agent-ready Open Knowledge Format", `"duckdb://tpcds/public"`, "item", "datasets/tpcds", "write tool"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt omits %q:\n%s", want, prompt)
		}
	}
}

func TestOKFGenDefaultsToTenMinutePerConceptTimeout(t *testing.T) {
	command := newOKFGenCommand(nil)
	flag := command.Flags().Lookup("timeout-ms")
	if flag == nil {
		t.Fatal("okfgen timeout flag is unavailable")
	}
	if got, want := flag.DefValue, "600000"; got != want {
		t.Fatalf("okfgen timeout default = %q, want %q", got, want)
	}
	debug := command.Flags().Lookup("debug")
	if debug == nil || debug.DefValue != "false" {
		t.Fatalf("okfgen debug flag = %#v, want false default", debug)
	}
}

func TestPiReferenceAgentUsesNonInteractivePrintMode(t *testing.T) {
	args := piReferenceAgentArgs("deepseek", "deepseek-v4-flash", "/tmp/extension.ts", "/tmp/skill", "skill instructions", "generate")
	for _, want := range []string{"--print", "--mode", "json", "--no-builtin-tools", "--extension", "/tmp/extension.ts", "--skill", "/tmp/skill", "--append-system-prompt", "skill instructions"} {
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Pi reference-agent arguments omit %q: %#v", want, args)
		}
	}
}

func TestPiDebugRendererRendersLiveConversationInsteadOfJSON(t *testing.T) {
	var output strings.Builder
	renderer := newPiDebugRenderer(&output)
	_, _ = renderer.Write([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"Planning"}}` + "\n"))
	_, _ = renderer.Write([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","toolCall":{"name":"list_concepts","arguments":{}}}}` + "\n"))
	_, _ = renderer.Write([]byte(`{"type":"tool_execution_start","toolName":"list_concepts","args":{}}` + "\n"))
	_, _ = renderer.Write([]byte(`{"type":"tool_execution_end","toolName":"list_concepts","isError":false}` + "\n"))
	renderer.Flush()
	got := output.String()
	for _, want := range []string{"[pi thinking] Planning", "[pi tool] → list_concepts({})", "[pi tool]   running list_concepts({})", "[pi tool] ← list_concepts: done"} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug trace omits %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"assistantMessageEvent"`) {
		t.Fatalf("debug trace leaked raw Pi JSON:\n%s", got)
	}
}

func TestReferenceAgentToolContextIncludesConceptPath(t *testing.T) {
	context := referenceAgentToolContext{Concept: referenceAgentConcept{ID: "tables/item", Path: "tables/item.md", Type: "DuckDB Table", Resource: "duckdb://tpcds/public/item"}, Semantic: json.RawMessage(`{"model":"tpcds","metrics":[{"name":"total_sales"}]}`)}
	body, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Concept struct {
			Path string `json:"path"`
		} `json:"concept"`
		Semantic struct {
			Metrics []struct {
				Name string `json:"name"`
			} `json:"metrics"`
		} `json:"semantic"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, want := decoded.Concept.Path, "tables/item.md"; got != want {
		t.Fatalf("extension concept path = %q, want %q", got, want)
	}
	if got, want := decoded.Semantic.Metrics[0].Name, "total_sales"; got != want {
		t.Fatalf("extension semantic metric = %q, want %q", got, want)
	}
}

func TestOKFSemanticContextScopesTableToOssieDefinitions(t *testing.T) {
	context := okfSemanticContext{Model: ossie.SemanticModel{
		Name: "tpcds",
		Datasets: []ossie.Dataset{
			{Name: "sales", Source: "tpcds.public.store_sales"},
			{Name: "date", Source: "tpcds.public.date_dim"},
		},
		Relationships: []ossie.Relationship{{Name: "sales_date", From: "sales", To: "date"}},
		Metrics: []ossie.Metric{
			{Name: "total_sales", Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "SUM(sales.amount)"}}}},
			{Name: "date_count", Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "COUNT(date.day)"}}}},
		},
	}}
	body, err := context.forConcept(referenceAgentConcept{ID: "tables/store_sales"})
	if err != nil {
		t.Fatal(err)
	}
	var scoped struct {
		Datasets      []ossie.Dataset      `json:"datasets"`
		Relationships []ossie.Relationship `json:"relationships"`
		Metrics       []ossie.Metric       `json:"metrics"`
	}
	if err := json.Unmarshal(body, &scoped); err != nil {
		t.Fatal(err)
	}
	if len(scoped.Datasets) != 1 || scoped.Datasets[0].Name != "sales" {
		t.Fatalf("scoped datasets = %#v", scoped.Datasets)
	}
	if len(scoped.Relationships) != 1 || scoped.Relationships[0].Name != "sales_date" {
		t.Fatalf("scoped relationships = %#v", scoped.Relationships)
	}
	if len(scoped.Metrics) != 1 || scoped.Metrics[0].Name != "total_sales" {
		t.Fatalf("scoped metrics = %#v", scoped.Metrics)
	}
}

func TestReferenceAgentWriteValidatorRejectsSampleSQL(t *testing.T) {
	if err := validateReferenceAgentBody("A source-backed concept without query recipes."); err != nil {
		t.Fatalf("non-SQL OKF body error = %v", err)
	}
	if err := validateReferenceAgentBody("# Common query patterns\n\n```sql\nSELECT * FROM public.item\n```"); err == nil || !strings.Contains(err.Error(), "must not include sample SQL") {
		t.Fatalf("sample SQL must be rejected: %v", err)
	}
}

func TestReferenceAgentDocumentsArePersistedAfterSourceAdapterRead(t *testing.T) {
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion,
		Engine:        "duckdb",
		EngineTitle:   "DuckDB",
		Database:      "tpcds",
		Schema:        "public",
		Generator:     "test",
		GeneratedAt:   "2026-09-03T00:00:00Z",
		Tables:        []readiness.CatalogTable{{Name: "item", Columns: []readiness.CatalogColumn{{Position: 1, Name: "i_item_sk", Type: "BIGINT"}}}},
	}
	projection, err := readiness.BuildCatalogProjection(catalog)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := projection.WriteBundle(root); err != nil {
		t.Fatal(err)
	}
	if err := readiness.ApplyReferenceAgentConceptDocuments(projection, map[string]readiness.ReferenceAgentConceptDocument{
		"tables/item.md": {Frontmatter: map[string]any{"type": "DuckDB Table"}, Body: "Semantic description from the generated Ossie model."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := projection.WriteBundle(root); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(root + "/tables/item.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Semantic description from the generated Ossie model.") {
		t.Fatalf("persisted OKF document omitted reference-agent body:\n%s", body)
	}
}

func TestLoadReferenceAgentWriteRequiresGoogleWriteShape(t *testing.T) {
	body, err := json.Marshal(referenceAgentWrite{ConceptID: "tables/item", Frontmatter: map[string]any{"type": "DuckDB Table"}, Body: "A concrete table."})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/write.json"
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	write, err := loadReferenceAgentWrite(path)
	if err != nil {
		t.Fatal(err)
	}
	if write.ConceptID != "tables/item" || write.Frontmatter["type"] != "DuckDB Table" {
		t.Fatalf("decoded write = %#v", write)
	}
}
