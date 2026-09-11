package command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

func TestClaudeSessionUsesOneProcessForMultipleQuestions(t *testing.T) {
	root := t.TempDir()
	pidLog := filepath.Join(root, "pids")
	script := filepath.Join(root, "fake-claude")
	body := `#!/bin/sh
set -eu
echo $$ >> "$FAKE_CLAUDE_PID_LOG"
IFS= read -r first
echo '{"type":"system","subtype":"init","model":"fake-model","mcp_servers":[]}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"SELECT 1","usage":{"input_tokens":10,"output_tokens":2}}'
IFS= read -r second
echo '{"type":"system","subtype":"init","model":"fake-model","mcp_servers":[]}'
echo '{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-2","content":[{"type":"tool_use","id":"tool-2","name":"Read"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"SELECT 2","usage":{"input_tokens":11,"output_tokens":3}}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{
		Workspace:   root,
		Prompt:      "question one",
		Environment: map[string]string{"FAKE_CLAUDE_PID_LOG": pidLog},
		Budget:      s2sbench.FrozenBudget,
	}
	driver := &claudeCodeDriver{binary: script, model: "fake-model"}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Prompt = "question two"
	second, err := session.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if first.Output != "SELECT 1" || second.Output != "SELECT 2" {
		t.Fatalf("outputs=%q/%q", first.Output, second.Output)
	}
	if first.ContextTokens != 10 || second.ContextTokens != 11 {
		t.Fatalf("per-turn input tokens=%d/%d, want 10/11", first.ContextTokens, second.ContextTokens)
	}
	if second.ToolCalls != 1 {
		t.Fatalf("second-turn customer MCP calls=%d, want 1", second.ToolCalls)
	}
	pids, err := os.ReadFile(pidLog)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Fields(string(pids)); len(lines) != 1 {
		t.Fatalf("agent process starts=%d, want 1: %q", len(lines), pids)
	}
}

func TestClaudeSessionClosesCleanlyAfterToolBudgetCutoff(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-claude")
	body := `#!/bin/sh
set -eu
IFS= read -r request
echo '{"type":"system","subtype":"init","model":"fake-model","mcp_servers":[]}'
echo '{"type":"assistant","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":1},"content":[{"type":"tool_use","id":"tool-1","name":"Read"},{"type":"tool_use","id":"tool-2","name":"Grep"}]}}'
exec sleep 30
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{
		Workspace: root,
		Prompt:    "question",
		Budget:    s2sbench.TaskBudget{ToolCalls: 1, Attempts: 1, Repetitions: 1},
	}
	driver := &claudeCodeDriver{binary: script, model: "fake-model"}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Run(context.Background(), request)
	if !s2sbench.IsToolBudgetExceededError(err) {
		t.Fatalf("Run error = %v, want tool-budget failure", err)
	}
	if result.ToolCalls != 2 || result.ContextTokens != 5 {
		t.Fatalf("cutoff result = %+v", result)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close after intentional cutoff: %v", err)
	}
}

func TestParseClaudeStreamCountsNativeHarnessToolsAndUsage(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[]}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"Read"},{"type":"tool_use","id":"tool-2","name":"Bash"},{"type":"tool_use","id":"tool-3","name":"Agent"}]}}`,
		`{"type":"system","subtype":"task_started"}`,
		`{"type":"assistant","parent_tool_use_id":"tool-3","message":{"id":"msg-sub","content":[{"type":"tool_use","id":"tool-4","name":"Grep"}]}}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"Read"}]}}`,
		`{"type":"user","message":{"content":[]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"SELECT 1","usage":{"input_tokens":10,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":4}}`,
	}, "\n") + "\n"

	parsed, err := parseClaudeStream(strings.NewReader(stream), 20, "claude-opus-4-6", "", nil)
	if err != nil {
		t.Fatalf("parseClaudeStream: %v", err)
	}
	if !parsed.InitSeen || !parsed.ResultSeen {
		t.Fatalf("init/result = %v/%v, want true/true", parsed.InitSeen, parsed.ResultSeen)
	}
	if parsed.Result.ToolCalls != 4 {
		t.Fatalf("tool calls = %d, want 4", parsed.Result.ToolCalls)
	}
	if parsed.Result.ContextTokens != 60 || parsed.Result.OutputTokens != 4 {
		t.Fatalf("usage = (%d,%d), want (60,4)", parsed.Result.ContextTokens, parsed.Result.OutputTokens)
	}
	if parsed.Result.Output != "SELECT 1" {
		t.Fatalf("output = %q", parsed.Result.Output)
	}
	if string(parsed.Result.Transcript) != stream {
		t.Fatalf("transcript changed\nwant: %q\n got: %q", stream, parsed.Result.Transcript)
	}
}

func TestParseClaudeStreamMetisArmAddsMCPWithoutRemovingHarnessTools(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[{"name":"metis","status":"connected"}]}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"Bash"},{"type":"tool_use","id":"tool-2","name":"mcp__metis__list_metrics","input":{"search":["order_amount"]}},{"type":"tool_use","id":"tool-3","name":"Agent"},{"type":"tool_use","id":"tool-4","name":"mcp__metis__compile","input":{"metrics":["average_order_amount"]}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-2","content":[{"type":"text","text":"metrics"}]},{"type":"tool_result","tool_use_id":"tool-4","content":"compile failed","is_error":true}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"SELECT 1","usage":{"input_tokens":10,"output_tokens":2}}`,
	}, "\n") + "\n"

	parsed, err := parseClaudeStream(strings.NewReader(stream), 20, "claude-opus-4-6", "metis", nil)
	if err != nil {
		t.Fatalf("parseClaudeStream: %v", err)
	}
	if parsed.Result.ToolCalls != 4 {
		t.Fatalf("tool calls = %d, want 4", parsed.Result.ToolCalls)
	}
	if len(parsed.Result.ToolTrace) != 2 {
		t.Fatalf("MCP trace = %+v, want two compact entries", parsed.Result.ToolTrace)
	}
	if parsed.Result.ToolTrace[0].Name != "metis.list_metrics" || parsed.Result.ToolTrace[0].RequestBytes == 0 || parsed.Result.ToolTrace[0].ResponseBytes == 0 || parsed.Result.ToolTrace[0].Status != "success" {
		t.Fatalf("search trace = %+v", parsed.Result.ToolTrace[0])
	}
	if parsed.Result.ToolTrace[1].Name != "metis.compile" || parsed.Result.ToolTrace[1].Status != "error" || parsed.Result.ToolTrace[1].Error != "compile failed" {
		t.Fatalf("compile trace = %+v", parsed.Result.ToolTrace[1])
	}
}

func TestParseClaudeStreamRetainsStructuredQueryMetricsResponse(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[{"name":"metis","status":"connected"}]}`,
		`{"type":"assistant","message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"mcp__metis__query_metrics","input":{"output_metrics":["metric:commerce.revenue"]}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":{"structuredContent":{"query_id":"q1","schema":{"columns":[{"name":"revenue","datatype":"Decimal"}]},"rows":[["350"]],"count":1}}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"queried\"}","usage":{"input_tokens":10,"output_tokens":2}}`,
	}, "\n") + "\n"
	parsed, err := parseClaudeStream(strings.NewReader(stream), 20, "claude-opus-4-6", "metis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Result.ToolTrace) != 1 || parsed.Result.ToolTrace[0].Response == "" || !strings.Contains(parsed.Result.ToolTrace[0].Response, `"query_id":"q1"`) {
		t.Fatalf("query_metrics trace = %#v", parsed.Result.ToolTrace)
	}
}

func TestParseClaudeStreamRawArmRejectsOnlyUnexpectedMCP(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[]}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"mcp__metis__list_metrics"}]}}`,
	}, "\n") + "\n"
	_, err := parseClaudeStream(strings.NewReader(stream), 20, "claude-opus-4-6", "", nil)
	if err == nil || !strings.Contains(err.Error(), "outside the isolated benchmark environment") {
		t.Fatalf("error = %v, want raw-arm MCP error", err)
	}
}

func TestParseClaudeStreamRejectsUnexpectedMCPInterface(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[{"name":"metis","status":"connected"}]}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"mcp__github__search"}]}}`,
	}, "\n") + "\n"
	_, err := parseClaudeStream(strings.NewReader(stream), 20, "claude-opus-4-6", "metis", nil)
	if err == nil || !strings.Contains(err.Error(), "outside the isolated benchmark environment") {
		t.Fatalf("error = %v, want unexpected-MCP error", err)
	}
}

func TestParseClaudeStreamStopsAtFirstToolBudgetOverrun(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[]}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","usage":{"input_tokens":3,"cache_read_input_tokens":4,"output_tokens":2},"content":[{"type":"tool_use","id":"tool-1","name":"Read"},{"type":"tool_use","id":"tool-2","name":"Bash"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"SELECT 1","usage":{"input_tokens":10,"output_tokens":2}}`,
	}, "\n") + "\n"
	stopped := false
	parsed, err := parseClaudeStream(strings.NewReader(stream), 1, "claude-opus-4-6", "", func() { stopped = true })
	if err == nil || !strings.Contains(err.Error(), "exceeded tool-call budget") {
		t.Fatalf("error = %v, want budget error", err)
	}
	if !stopped {
		t.Fatal("budget overrun did not stop the Claude process")
	}
	if parsed.Result.ToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2", parsed.Result.ToolCalls)
	}
	if parsed.Result.ContextTokens != 7 || parsed.Result.OutputTokens != 2 {
		t.Fatalf("usage at cutoff = (%d,%d), want (7,2)", parsed.Result.ContextTokens, parsed.Result.OutputTokens)
	}
}

func TestParseClaudeStreamDoesNotDrainAfterToolBudgetOverrun(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[]}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"Read"},{"type":"tool_use","id":"tool-2","name":"Bash"}]}}`,
		`{"type":"future_event"}`,
	}, "\n") + "\n"

	parsed, err := parseClaudeStream(strings.NewReader(stream), 1, "claude-opus-4-6", "", nil)
	if !s2sbench.IsToolBudgetExceededError(err) {
		t.Fatalf("error = %v, want tool-budget failure", err)
	}
	if strings.Contains(err.Error(), "unrecognized Claude Code event type") {
		t.Fatalf("budget cutoff drained later protocol event: %v", err)
	}
	if parsed.Result.ToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2", parsed.Result.ToolCalls)
	}
}

func TestParseClaudeStreamValidatesPinnedModelAndMCP(t *testing.T) {
	tests := []struct {
		name        string
		stream      string
		expectedMCP string
		want        string
	}{
		{
			name:   "model drift",
			stream: `{"type":"system","subtype":"init","model":"claude-sonnet-4-6","mcp_servers":[]}` + "\n",
			want:   "want pinned",
		},
		{
			name:        "missing mcp",
			stream:      `{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[]}` + "\n",
			expectedMCP: "metis",
			want:        "did not report required MCP",
		},
		{
			name:        "failed mcp",
			stream:      `{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[{"name":"metis","status":"failed"}]}` + "\n",
			expectedMCP: "metis",
			want:        "want connected",
		},
		{
			name:        "extra mcp",
			stream:      `{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[{"name":"metis","status":"connected"},{"name":"other","status":"connected"}]}` + "\n",
			expectedMCP: "metis",
			want:        "want isolated benchmark environment",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseClaudeStream(strings.NewReader(tt.stream), 20, "claude-opus-4-6", tt.expectedMCP, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseClaudeStreamAllowsBackgroundLifecycleButRejectsAmbiguousProtocol(t *testing.T) {
	for _, subtype := range []string{"api_retry", "thinking_tokens", "task_started", "task_progress", "task_updated", "task_notification", "permission_denied"} {
		stream := strings.Join([]string{
			`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[]}`,
			`{"type":"system","subtype":"` + subtype + `"}`,
			`{"type":"result","subtype":"success","is_error":false,"result":"SELECT 1","usage":{"input_tokens":1,"output_tokens":1}}`,
		}, "\n") + "\n"
		if _, err := parseClaudeStream(strings.NewReader(stream), 20, "claude-opus-4-6", "", nil); err != nil {
			t.Fatalf("subtype %s should be allowed: %v", subtype, err)
		}
	}

	for _, event := range []struct {
		name string
		line string
		want string
	}{
		{name: "partial", line: `{"type":"stream_event"}`, want: "partial stream"},
		{name: "unknown", line: `{"type":"future_event"}`, want: "unrecognized Claude Code event type"},
	} {
		t.Run(event.name, func(t *testing.T) {
			_, err := parseClaudeStream(strings.NewReader(event.line+"\n"), 20, "claude-opus-4-6", "", nil)
			if err == nil || !strings.Contains(err.Error(), event.want) {
				t.Fatalf("error = %v, want %q", err, event.want)
			}
		})
	}
}

func TestClaudeExecArgsPreapproveOnlyBenchmarkMCP(t *testing.T) {
	rawArgs := strings.Join(claudeExecArgs("claude-opus-4-6", "/tmp/raw-mcp.json", ""), " ")
	if strings.Contains(rawArgs, "--allowedTools") {
		t.Fatalf("raw args unexpectedly preapprove tools: %s", rawArgs)
	}
	for _, want := range []string{
		"--strict-mcp-config",
		"--no-chrome",
		"--disable-slash-commands",
		"--tools Read,Grep,Glob",
		"--input-format stream-json",
		"--output-format stream-json",
		"--no-session-persistence",
		"--mcp-config /tmp/raw-mcp.json",
		"--permission-mode bypassPermissions",
	} {
		if !strings.Contains(rawArgs, want) {
			t.Fatalf("raw args = %q, missing %q", rawArgs, want)
		}
	}
	if strings.Contains(rawArgs, "--safe-mode") {
		t.Fatalf("raw args use safe mode, which disables the benchmark MCP: %s", rawArgs)
	}
	metisArgs := strings.Join(claudeExecArgs("claude-opus-4-6", "/tmp/metis-mcp.json", "metis"), " ")
	if !strings.Contains(metisArgs, "--tools Read,Grep,Glob") || strings.Contains(metisArgs, "--disallowedTools") {
		t.Fatalf("Metis args do not retain the same built-in tools as raw: %s", metisArgs)
	}
	if strings.Contains(metisArgs, "--allowedTools") || strings.Contains(metisArgs, "mcp__metis__") {
		t.Fatalf("Metis args freeze an MCP tool allowlist instead of using server discovery: %s", metisArgs)
	}
}

func TestParseClaudeStreamAcceptsServerDiscoveredBenchmarkMCPTool(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-4-6","mcp_servers":[{"name":"metis","status":"connected"}]}`,
		`{"type":"assistant","message":{"id":"msg-1","content":[{"type":"tool_use","id":"tool-1","name":"mcp__metis__future_tool","input":{}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"input_tokens":1,"output_tokens":1}}`,
	}, "\n") + "\n"
	parsed, err := parseClaudeStream(strings.NewReader(stream), 10, "claude-opus-4-6", "metis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Result.ToolCalls != 1 {
		t.Fatalf("server-discovered tool was not counted: %+v", parsed.Result)
	}
}

func TestClaudeCommandEnvironmentImportsOnlyProviderSettings(t *testing.T) {
	userHome := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", userHome)
	settingsDir := filepath.Join(userHome, ".claude")
	if err := os.Mkdir(settingsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"env":{"ANTHROPIC_AUTH_TOKEN":"test-token","ANTHROPIC_BASE_URL":"https://example.invalid","UNRELATED_PLUGIN_VALUE":"leak"},"enabledPlugins":{"example":true}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	isolatedHome, err := prepareIsolatedAgentHome("s2sbench-claude-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(isolatedHome) })
	environment, err := claudeCommandEnvironment(s2sbench.AgentRequest{
		Workspace:   workspace,
		Environment: map[string]string{"METIS_TEST_TOKEN": "metis-token"},
	}, isolatedHome)
	if err != nil {
		t.Fatal(err)
	}
	values := environmentMap(environment)
	if values["HOME"] != isolatedHome || values["ANTHROPIC_AUTH_TOKEN"] != "test-token" || values["METIS_TEST_TOKEN"] != "metis-token" {
		t.Fatalf("isolated Claude environment missing required values: %v", values)
	}
	if _, leaked := values["UNRELATED_PLUGIN_VALUE"]; leaked {
		t.Fatal("isolated Claude environment imported unrelated user settings")
	}
}

func TestWriteClaudeMCPConfigUsesEnvironmentExpansion(t *testing.T) {
	path, name, err := writeClaudeMCPConfig(&s2sbench.AgentMCP{
		Name:              "metis",
		URL:               "http://127.0.0.1:18080/mcp",
		BearerTokenEnvVar: "METIS_S2SBENCH_TOKEN",
		HTTPHeaders:       map[string]string{"X-Metis-Project-Id": "finance"},
	})
	if err != nil {
		t.Fatalf("writeClaudeMCPConfig: %v", err)
	}
	defer os.Remove(path)
	if name != "metis" {
		t.Fatalf("name = %q, want metis", name)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var config struct {
		Servers map[string]claudeMCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	server, ok := config.Servers["metis"]
	if !ok {
		t.Fatalf("config = %s, missing metis", body)
	}
	if got := server.Headers["Authorization"]; got != "Bearer ${METIS_S2SBENCH_TOKEN}" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := server.Headers["X-Metis-Project-Id"]; got != "finance" {
		t.Fatalf("project header = %q", got)
	}
}
