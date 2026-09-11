package s2sbench

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestNormalizeAgentOutputRawPreservesSQL(t *testing.T) {
	got, err := normalizeAgentOutput(PathRawAssets, "  SELECT 1;  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != "SELECT 1;" {
		t.Fatalf("got %q, want trimmed SQL", got)
	}
}

func TestNormalizeAgentOutputRawUnwrapsSQLFence(t *testing.T) {
	got, err := normalizeAgentOutput(PathRawAssets, "```sql\nSELECT 1;\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != "SELECT 1;" {
		t.Fatalf("got %q, want unwrapped SQL", got)
	}
}

func TestNormalizeAgentOutputMetisPreservesPhysicalQueryParameters(t *testing.T) {
	output := `{"dialect":"DUCKDB","sql":"SELECT ? AS region, ? AS enabled","parameters":[{"value":"APAC"},{"value":true}]}`
	got, err := normalizeAgentOutput(PathMetis, output)
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != "SELECT ? AS region, ? AS enabled" || len(got.Parameters) != 2 || got.Parameters[0].Value != "APAC" || got.Parameters[1].Value != true {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeAgentOutputMetisUnwrapsJSONFence(t *testing.T) {
	output := "```json\n{\"dialect\":\"DUCKDB\",\"sql\":\"SELECT 1\"}\n```"
	got, err := normalizeAgentOutput(PathMetis, output)
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != "SELECT 1" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeAgentOutputMetisFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "wrong dialect", output: `{"dialect":"DORIS","sql":"SELECT 1"}`, want: "want DUCKDB"},
		{name: "prose wrapper", output: "Here is the result:\n```json\n{\"dialect\":\"DUCKDB\",\"sql\":\"SELECT 1\"}\n```", want: "malformed code fence"},
		{name: "wrong fence", output: "```sql\n{\"dialect\":\"DUCKDB\",\"sql\":\"SELECT 1\"}\n```", want: "want \"json\""},
		{name: "unknown field", output: `{"dialect":"DUCKDB","sql":"SELECT 1","extra":true}`, want: "unknown field"},
		{name: "two values", output: `{"dialect":"DUCKDB","sql":"SELECT 1"} {"dialect":"DUCKDB","sql":"SELECT 2"}`, want: "more than one JSON value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeAgentOutput(PathMetis, tt.output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeAgentOutputMetisIncludesBoundedInvalidOutputPrefix(t *testing.T) {
	output := "<tool_call>" + strings.Repeat("x", 300)
	_, err := normalizeAgentOutput(PathMetis, output)
	if err == nil || !strings.Contains(err.Error(), "<tool_call>") {
		t.Fatalf("error = %v, want invalid output prefix", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("x", 300)) {
		t.Fatalf("error contains unbounded model output: %v", err)
	}
}

func TestNormalizeAgentOutputMetisExplainsMalformedTrailingContent(t *testing.T) {
	output := `{"dialect":"DUCKDB","sql":"SELECT 1"} {"dialect"`
	_, err := normalizeAgentOutput(PathMetis, output)
	if err == nil {
		t.Fatal("expected malformed trailing content to fail")
	}
	for _, want := range []string{
		"one valid Metis sql_statement JSON object",
		"return only the first complete object",
		`trailing prefix="{\"dialect\""`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want substring %q", err, want)
		}
	}
}

func TestExternalAgentPathCollectsCompleteFrozenDualArmEvidence(t *testing.T) {
	manifest, err := NewAgentRunManifest(
		AgentIdentity{Name: "fake-agent", Version: "v1", Model: "fake-model"},
		"deterministic-test",
		"fake-model",
		"v1",
	)
	if err != nil {
		t.Fatalf("NewAgentRunManifest: %v", err)
	}

	driver := &deterministicAgentDriver{}
	tasks := &deterministicAgentTaskFactory{}
	rawExecution := &deterministicAgentExecution{}
	metisExecution := &deterministicAgentExecution{}

	collection, err := Collect(
		context.Background(),
		manifest,
		&AgentRunner{Arm: PathRawAssets, Driver: driver, Tasks: tasks},
		rawExecution,
		&AgentRunner{Arm: PathMetis, Driver: driver, Tasks: tasks},
		metisExecution,
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantPerArm := len(manifest.Scenarios) * manifest.Budget.Repetitions
	if len(collection.RawAssets.Attempts) != wantPerArm || len(collection.Metis.Attempts) != wantPerArm {
		t.Fatalf("attempts raw=%d metis=%d, want %d each", len(collection.RawAssets.Attempts), len(collection.Metis.Attempts), wantPerArm)
	}
	if driver.rawCalls != wantPerArm || driver.metisCalls != wantPerArm {
		t.Fatalf("agent calls raw=%d metis=%d, want %d each", driver.rawCalls, driver.metisCalls, wantPerArm)
	}
	wantSessions := 2 * wantPerArm
	if driver.openCalls != wantSessions || driver.closeCalls != wantSessions {
		t.Fatalf("agent sessions opened/closed = %d/%d, want %d (one per arm, question, and repetition)", driver.openCalls, driver.closeCalls, wantSessions)
	}
	for i, calls := range driver.sessionCalls {
		if calls != 1 {
			t.Fatalf("agent session %d handled %d questions, want exactly 1", i, calls)
		}
	}
	if tasks.rawCalls != wantPerArm || tasks.metisCalls != wantPerArm {
		t.Fatalf("task builds raw=%d metis=%d, want %d each", tasks.rawCalls, tasks.metisCalls, wantPerArm)
	}
	wantPrepares := len(manifest.Scenarios) * manifest.Budget.Repetitions
	if rawExecution.prepareCalls != wantPrepares || metisExecution.prepareCalls != wantPrepares {
		t.Fatalf("prepare calls raw=%d metis=%d, want %d each", rawExecution.prepareCalls, metisExecution.prepareCalls, wantPrepares)
	}

	for _, artifact := range []ArmRunArtifact{collection.RawAssets, collection.Metis} {
		for i, record := range artifact.Attempts {
			if record.Verdict != VerdictCorrect || record.Index != 1 {
				t.Fatalf("%s attempt %d verdict/index = %s/%d, want correct/1", artifact.Path, i, record.Verdict, record.Index)
			}
			if record.Transcript == "" || len(record.Trace) != 1 || record.Trace[0].Transcript == "" {
				t.Fatalf("%s attempt %d lost in-memory external-agent protocol evidence", artifact.Path, i)
			}
		}
	}

	report, err := BuildV0Report(collection)
	if err != nil {
		t.Fatalf("BuildV0Report: %v", err)
	}
	for _, stratum := range Strata {
		if got := report.RawAssets.Summary.ByStratum[stratum]; got.Wrong != 0 || got.Failed != 0 {
			t.Fatalf("raw stratum %s = %+v", stratum, got)
		}
		if got := report.Metis.Summary.ByStratum[stratum]; got.Wrong != 0 || got.Failed != 0 {
			t.Fatalf("Metis stratum %s = %+v", stratum, got)
		}
	}
}

type deterministicAgentTaskFactory struct {
	rawCalls   int
	metisCalls int
}

func (f *deterministicAgentTaskFactory) BuildAgentRequest(_ context.Context, path Path, question string, _ scenarios.Scenario, budget TaskBudget, failure string) (AgentRequest, error) {
	if failure != "" {
		return AgentRequest{}, fmt.Errorf("deterministic full-collection path unexpectedly requested repair: %s", failure)
	}
	request := AgentRequest{Workspace: "deterministic-workspace", Prompt: question, Budget: budget}
	switch path {
	case PathRawAssets:
		f.rawCalls++
	case PathMetis:
		f.metisCalls++
		request.MCP = &AgentMCP{Name: "metis", URL: "http://127.0.0.1:1"}
	default:
		return AgentRequest{}, fmt.Errorf("unknown path %q", path)
	}
	return request, nil
}

type deterministicAgentDriver struct {
	rawCalls     int
	metisCalls   int
	openCalls    int
	closeCalls   int
	currentCalls int
	sessionCalls []int
}

func (d *deterministicAgentDriver) Identity(context.Context) (AgentIdentity, error) {
	return AgentIdentity{Name: "fake-agent", Version: "v1", Model: "fake-model"}, nil
}

func (d *deterministicAgentDriver) Open(context.Context, AgentRequest) (AgentSession, error) {
	d.openCalls++
	d.currentCalls = 0
	return d, nil
}

func (d *deterministicAgentDriver) Close() error {
	d.closeCalls++
	d.sessionCalls = append(d.sessionCalls, d.currentCalls)
	return nil
}

func (d *deterministicAgentDriver) Run(_ context.Context, request AgentRequest) (AgentResult, error) {
	d.currentCalls++
	result := AgentResult{
		ToolCalls:     1,
		ContextTokens: 10,
		OutputTokens:  2,
		Transcript:    []byte(`{"type":"deterministic-agent-event"}` + "\n"),
	}
	if request.MCP == nil {
		d.rawCalls++
		result.Output = "SELECT 1"
		return result, nil
	}
	d.metisCalls++
	result.Output = `{"dialect":"DUCKDB","sql":"SELECT 1"}`
	return result, nil
}

type deterministicAgentExecution struct {
	current      scenarios.Scenario
	prepareCalls int
}

func (e *deterministicAgentExecution) Prepare(_ context.Context, scenario scenarios.Scenario) error {
	if scenario.ExpectedResult == nil {
		return fmt.Errorf("scenario %q has no canonical result expectation", scenario.Name)
	}
	e.current = scenario
	e.prepareCalls++
	return nil
}

func (e *deterministicAgentExecution) RunSQL(context.Context, string, ...sql.QueryParameter) (scenarios.ResultSet, error) {
	return e.current.ExpectedResult.ResultSet, nil
}
