package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

type staticSchema string

func (s staticSchema) PhysicalSchema(context.Context) (string, error) { return string(s), nil }

type mutableSchema struct{ value string }

func (s *mutableSchema) PhysicalSchema(context.Context) (string, error) { return s.value, nil }

type staticEndpoint struct{}

func (staticEndpoint) CurrentEndpoint() (s2sbench.AgentMCP, string, map[string]string, error) {
	return s2sbench.AgentMCP{Name: "metis", URL: "http://127.0.0.1:12345", BearerTokenEnvVar: "TOKEN", HTTPHeaders: map[string]string{"X-Metis-Project-Id": fixtures.ConformanceProject}}, fixtures.ConformanceProject, map[string]string{"TOKEN": "secret"}, nil
}

func TestWorkspaceFactoryChangesOnlySemanticInterface(t *testing.T) {
	models := canonicalModels(t)
	root := semanticProjectForTest(t, models)
	metisRoot := t.TempDir()
	factory, err := newWorkspaceFactory(root, metisRoot, staticSchema("CREATE TABLE analytics.orders (amount DOUBLE);\n"), staticEndpoint{})
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios.Scenario{Name: "workspace-test", Fixture: fixtures.Commerce}

	raw, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathRawAssets, "question", scenario, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(raw.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 || entries[0].Name() != "metis.yaml" || entries[1].Name() != "models" || entries[2].Name() != "project.yaml" || entries[3].Name() != "schema.sql" {
		t.Fatalf("raw project entries=%v, want metis.yaml, models, project.yaml, and schema.sql", entryNames(entries))
	}
	modelEntries, err := os.ReadDir(filepath.Join(raw.Workspace, "models"))
	if err != nil {
		t.Fatal(err)
	}
	if len(modelEntries) != len(models) {
		t.Fatalf("raw models=%d, want %d", len(modelEntries), len(models))
	}
	for _, definition := range models {
		path := filepath.Join(raw.Workspace, "models", sanitizeWorkspaceComponent(definition.Model)+".ossie.yaml")
		model, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(model) != string(definition.Document) {
			t.Fatalf("raw model %q differs from canonical fixture", definition.Model)
		}
	}
	if raw.MCP != nil || len(raw.Environment) != 0 {
		t.Fatalf("raw arm unexpectedly exposes MCP credentials: MCP=%#v env=%v", raw.MCP, raw.Environment)
	}
	if raw.Budget != s2sbench.FrozenBudget {
		t.Fatalf("raw budget=%+v, want %+v", raw.Budget, s2sbench.FrozenBudget)
	}
	budgetText := fmt.Sprintf("at most %d tool calls in this turn", s2sbench.FrozenBudget.ToolCalls)
	if !strings.Contains(raw.Prompt, budgetText) {
		t.Fatalf("raw prompt does not disclose attempt budget: %s", raw.Prompt)
	}
	timeText := fmt.Sprintf("%d-second wall-clock limit", s2sbench.FrozenBudget.QuestionTimeoutMS/1000)
	if !strings.Contains(raw.Prompt, timeText) {
		t.Fatalf("raw prompt does not disclose question timeout: %s", raw.Prompt)
	}
	for _, coaching := range []string{"targeted discovery", "exhaustive enumeration", "repeated candidate probing", "sufficient evidence"} {
		if strings.Contains(raw.Prompt, coaching) {
			t.Fatalf("raw prompt contains benchmark-specific Agent coaching %q: %s", coaching, raw.Prompt)
		}
	}

	metis, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathMetis, "question", scenario, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	if metis.MCP == nil || metis.MCP.Name != "metis" || metis.Environment["TOKEN"] != "secret" {
		t.Fatalf("Metis endpoint/credentials=%#v %v", metis.MCP, metis.Environment)
	}
	if metis.Budget != raw.Budget {
		t.Fatalf("arm budgets differ: raw=%+v metis=%+v", raw.Budget, metis.Budget)
	}
	if !strings.Contains(metis.Prompt, budgetText) {
		t.Fatalf("Metis prompt does not disclose attempt budget: %s", metis.Prompt)
	}
	for _, coaching := range []string{"targeted discovery", "exhaustive enumeration", "repeated candidate probing", "sufficient evidence"} {
		if strings.Contains(metis.Prompt, coaching) {
			t.Fatalf("Metis prompt contains benchmark-specific Agent coaching %q: %s", coaching, metis.Prompt)
		}
	}
	if !strings.Contains(metis.Prompt, timeText) {
		t.Fatalf("Metis prompt does not disclose question timeout: %s", metis.Prompt)
	}
	if raw.Workspace != root || metis.Workspace == raw.Workspace || filepath.Dir(metis.Workspace) != metisRoot {
		t.Fatalf("arm workspaces do not isolate semantic files: raw=%q metis=%q project=%q clean-root=%q", raw.Workspace, metis.Workspace, root, metisRoot)
	}
	metisEntries, err := os.ReadDir(metis.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(metisEntries) != 0 {
		t.Fatalf("Metis workspace contains semantic or stale assets: %v", entryNames(metisEntries))
	}
	if strings.Contains(metis.Prompt, "project: "+fixtures.ConformanceProject) || metis.MCP.HTTPHeaders["X-Metis-Project-Id"] != fixtures.ConformanceProject {
		t.Fatalf("configured project context was not bound outside the prompt: prompt=%s MCP=%#v", metis.Prompt, metis.MCP)
	}
	if strings.Contains(metis.Prompt, "model:") || strings.Contains(metis.Prompt, fixtures.CommerceModel) {
		t.Fatalf("Metis prompt leaks preselected model: %s", metis.Prompt)
	}

	repair, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathMetis, "question", scenario, s2sbench.FrozenBudget, "execution failed")
	if err != nil {
		t.Fatal(err)
	}
	if repair.Workspace != metis.Workspace {
		t.Fatal("repair did not reuse the arm session workspace")
	}
	if !strings.Contains(repair.Prompt, "execution failed") {
		t.Fatal("repair prompt did not contain the prior failure")
	}
}

func TestWorkspaceFactoryColdStartLeavesProjectUnbound(t *testing.T) {
	factory, err := newWorkspaceFactory(semanticProjectForTest(t, canonicalModels(t)), t.TempDir(), staticSchema("CREATE TABLE t (x INT);"), staticEndpoint{})
	if err != nil {
		t.Fatal(err)
	}
	factory.WithBoundProjectContext(false)
	request, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathMetis, "question", scenarios.Scenario{Name: "cold", Fixture: fixtures.Commerce}, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.MCP.HTTPHeaders) != 0 || strings.Contains(request.Prompt, fixtures.ConformanceProject) || !strings.Contains(request.Prompt, "has not bound an active project context") {
		t.Fatalf("cold-start request=%#v prompt=%s", request.MCP, request.Prompt)
	}
}

func TestWorkspaceFactoryStartsEachQuestionWithFreshSessionContext(t *testing.T) {
	root := semanticProjectForTest(t, canonicalModels(t))
	schema := &mutableSchema{value: "CREATE TABLE first_table (x INT);"}
	factory, err := newWorkspaceFactory(root, t.TempDir(), schema, staticEndpoint{})
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios.Scenario{Name: "session", Fixture: fixtures.Commerce}
	if err := factory.BeginSession(1); err != nil {
		t.Fatal(err)
	}
	first, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathRawAssets, "q1", scenario, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	repair, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathRawAssets, "q1", scenario, s2sbench.FrozenBudget, "execution failed")
	if err != nil {
		t.Fatal(err)
	}
	if first.Workspace != repair.Workspace {
		t.Fatal("repair did not reuse its question workspace")
	}
	if !strings.Contains(first.Prompt, s2sbench.RawAssetsSystemPrompt) || strings.Contains(repair.Prompt, s2sbench.RawAssetsSystemPrompt) {
		t.Fatal("full protocol was not limited to the first turn of the question")
	}
	if err := factory.BeginSession(1); err != nil {
		t.Fatal(err)
	}
	schema.value = "CREATE TABLE second_table (x INT);"
	modelPath := filepath.Join(first.Workspace, "models", fixtures.CommerceModel+".ossie.yaml")
	before, err := os.Stat(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	next, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathRawAssets, "q2", scenario, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.Workspace != first.Workspace {
		t.Fatal("new question did not reuse the arm workspace")
	}
	if !strings.Contains(next.Prompt, s2sbench.RawAssetsSystemPrompt) {
		t.Fatal("new question did not restore the full first-turn protocol")
	}
	after, err := os.Stat(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("new question rebuilt the shared semantic model assets")
	}
	schemaBody, err := os.ReadFile(filepath.Join(next.Workspace, "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(schemaBody) != schema.value {
		t.Fatalf("shared schema = %q, want current fixture %q", schemaBody, schema.value)
	}
}

func TestWorkspaceFactoryGivesEachMetisQuestionAFreshEmptyWorkspace(t *testing.T) {
	root := semanticProjectForTest(t, canonicalModels(t))
	metisRoot := t.TempDir()
	factory, err := newWorkspaceFactory(root, metisRoot, staticSchema("CREATE TABLE t (x INT);"), staticEndpoint{})
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios.Scenario{Name: "metis-workspace", Fixture: fixtures.Commerce}

	if err := factory.BeginSession(1); err != nil {
		t.Fatal(err)
	}
	first, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathMetis, "q1", scenario, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Workspace, "agent-note.txt"), []byte("question one"), 0o600); err != nil {
		t.Fatal(err)
	}
	repair, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathMetis, "q1", scenario, s2sbench.FrozenBudget, "execution failed")
	if err != nil {
		t.Fatal(err)
	}
	if repair.Workspace != first.Workspace {
		t.Fatal("same-question repair did not reuse its Metis workspace")
	}

	if err := factory.BeginSession(1); err != nil {
		t.Fatal(err)
	}
	next, err := factory.BuildAgentRequest(context.Background(), s2sbench.PathMetis, "q2", scenario, s2sbench.FrozenBudget, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.Workspace == first.Workspace {
		t.Fatal("independent Metis questions reused a workspace")
	}
	entries, err := os.ReadDir(next.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("new Metis question inherited workspace state: %v", entryNames(entries))
	}
	if _, err := os.Stat(filepath.Join(next.Workspace, "metis.yaml")); !os.IsNotExist(err) {
		t.Fatalf("new Metis workspace exposes metis.yaml: %v", err)
	}
}

func semanticProjectForTest(t *testing.T, models []fixtures.Definition) string {
	t.Helper()
	root := t.TempDir()
	if _, _, err := writeSemanticProject(root, models); err != nil {
		t.Fatal(err)
	}
	return root
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
