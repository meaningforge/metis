package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

func TestCodexAppSessionUsesOneProcessForMultipleQuestions(t *testing.T) {
	root := t.TempDir()
	pidLog := filepath.Join(root, "pids")
	script := filepath.Join(root, "fake-codex")
	body := `#!/bin/sh
set -eu
echo $$ >> "$FAKE_CODEX_PID_LOG"
IFS= read -r initialize
echo '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}'
IFS= read -r initialized
IFS= read -r mcp_status
echo '{"id":2,"result":{"data":[{"name":"metis"}],"nextCursor":null}}'
IFS= read -r thread_start
echo '{"id":3,"result":{"thread":{"id":"thread-1"}}}'
for id in 4 5; do
  IFS= read -r turn_start
  echo "{\"id\":$id,\"result\":{\"turn\":{\"id\":\"turn-$id\"}}}"
  echo "{\"method\":\"thread/tokenUsage/updated\",\"params\":{\"threadId\":\"thread-1\",\"turnId\":\"turn-$id\",\"tokenUsage\":{\"last\":{\"inputTokens\":10,\"outputTokens\":2}}}}"
  if [ "$id" = 4 ]; then
    echo "{\"method\":\"item/completed\",\"params\":{\"threadId\":\"thread-1\",\"turnId\":\"turn-$id\",\"item\":{\"id\":\"mcp-$id\",\"type\":\"mcpToolCall\",\"server\":\"metis\",\"tool\":\"list_metrics\",\"status\":\"completed\",\"arguments\":{\"search\":[\"revenue\"]},\"result\":{\"content\":[]},\"durationMs\":4}}}"
  fi
  echo "{\"method\":\"item/completed\",\"params\":{\"threadId\":\"thread-1\",\"turnId\":\"turn-$id\",\"item\":{\"id\":\"answer-$id\",\"type\":\"agentMessage\",\"text\":\"SELECT $id\"}}}"
  echo "{\"method\":\"turn/completed\",\"params\":{\"threadId\":\"thread-1\",\"turn\":{\"id\":\"turn-$id\",\"status\":\"completed\"}}}"
done
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{
		Workspace:   root,
		Prompt:      "question one",
		Environment: map[string]string{"FAKE_CODEX_PID_LOG": pidLog},
		Budget:      s2sbench.FrozenBudget,
		MCP:         &s2sbench.AgentMCP{Name: "metis", URL: "http://127.0.0.1:1/mcp"},
	}
	driver := &codexDriver{binary: script, model: "fake-model"}
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
	if first.Output != "SELECT 4" || second.Output != "SELECT 5" {
		t.Fatalf("outputs=%q/%q", first.Output, second.Output)
	}
	if first.ContextTokens != 10 || second.ContextTokens != 10 {
		t.Fatalf("per-turn input tokens=%d/%d, want 10/10", first.ContextTokens, second.ContextTokens)
	}
	if first.ToolCalls != 1 || second.ToolCalls != 0 || len(first.ToolTrace) != 1 || first.ToolTrace[0].Name != "metis.list_metrics" {
		t.Fatalf("per-turn tool evidence first=%+v second=%+v", first, second)
	}
	pids, err := os.ReadFile(pidLog)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Fields(string(pids)); len(lines) != 1 {
		t.Fatalf("agent process starts=%d, want 1: %q", len(lines), pids)
	}
}

func TestCodexAppSessionStopsAndReturnsCompleteTraceAtFirstToolOverrun(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-codex-overrun")
	body := `#!/bin/sh
set -eu
IFS= read -r initialize
echo '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}'
IFS= read -r initialized
IFS= read -r mcp_status
echo '{"id":2,"result":{"data":[{"name":"metis"}],"nextCursor":null}}'
IFS= read -r thread_start
echo '{"id":3,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r turn_start
echo '{"id":4,"result":{"turn":{"id":"turn-4"}}}'
echo '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-4","item":{"id":"mcp-1","type":"mcpToolCall","server":"metis","tool":"list_models","status":"completed","arguments":{},"result":{"content":[]},"error":null,"durationMs":2}}}'
echo '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-4","item":{"id":"mcp-2","type":"mcpToolCall","server":"metis","tool":"list_metrics","status":"completed","arguments":{},"result":{"content":[]},"error":null,"durationMs":3}}}'
echo '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-4","status":"completed"}}}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{
		Workspace: root,
		Prompt:    "question",
		Budget:    s2sbench.TaskBudget{ToolCalls: 1, Attempts: 1, QuestionTimeoutMS: 1000, Repetitions: 1},
		MCP:       &s2sbench.AgentMCP{Name: "metis", URL: "http://127.0.0.1:1/mcp"},
	}
	session, err := (&codexDriver{binary: script, model: "fake-model"}).Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := session.Run(context.Background(), request)
	if closeErr := session.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	var exceeded *s2sbench.ToolBudgetExceededError
	if !errors.As(runErr, &exceeded) || exceeded.Used != 2 || exceeded.Budget != 1 {
		t.Fatalf("Run error = %v, want tool budget 2/1", runErr)
	}
	if result.ToolCalls != 2 || len(result.ToolTrace) != 2 {
		t.Fatalf("tool evidence = calls:%d trace:%+v, want two complete calls", result.ToolCalls, result.ToolTrace)
	}
	for i, evidence := range result.ToolTrace {
		if evidence.Status != "success" {
			t.Fatalf("trace %d status = %q, want success for completed item with null error", i, evidence.Status)
		}
	}
	transcript := string(result.Transcript)
	if !strings.Contains(transcript, `"id":"mcp-1"`) || !strings.Contains(transcript, `"id":"mcp-2"`) {
		t.Fatalf("terminal transcript is incomplete: %s", transcript)
	}
}

func TestCodexAppSessionPersistsBoundedMCPErrorSummary(t *testing.T) {
	got := toolErrorSummary(json.RawMessage(`{"code":-32602,"message":"invalid grain"}`))
	if got != `{"code":-32602,"message":"invalid grain"}` {
		t.Fatalf("tool error summary = %q", got)
	}
	if got := toolErrorSummary(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null tool error summary = %q, want empty", got)
	}
}

func TestCodexAppSessionRejectsPreconfiguredMetisInRawArm(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-codex-with-leaked-mcp")
	body := `#!/bin/sh
set -eu
IFS= read -r initialize
echo '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}'
IFS= read -r initialized
IFS= read -r mcp_status
echo '{"id":2,"result":{"data":[{"name":"metis"}],"nextCursor":null}}'
IFS= read -r shutdown
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &codexDriver{binary: script, model: "fake-model"}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "question", Budget: s2sbench.FrozenBudget}
	if _, err := driver.Open(context.Background(), request); err == nil || !strings.Contains(err.Error(), "isolated raw") {
		t.Fatalf("Open error=%v, want preconfigured-Metis failure", err)
	}
}

func TestMCPEnvironmentAllowsOnlyIsolatedRawAndMetis(t *testing.T) {
	var baseline mcpEnvironmentBaseline
	if got, err := baseline.Validate(nil, ""); err != nil || len(got) != 0 {
		t.Fatalf("isolated raw environment got=%v err=%v", got, err)
	}
	if _, err := baseline.Validate([]string{"metis"}, "metis"); err != nil {
		t.Fatalf("isolated Metis environment: %v", err)
	}
	if _, err := baseline.Validate([]string{"github"}, ""); err == nil || !strings.Contains(err.Error(), "want isolated") {
		t.Fatalf("unexpected raw environment error=%v", err)
	}
	if _, err := baseline.Validate([]string{"metis", "other"}, "metis"); err == nil || !strings.Contains(err.Error(), "want isolated") {
		t.Fatalf("unexpected Metis environment error=%v", err)
	}
}

func TestMCPEnvironmentBaselineRejectsPreconfiguredMetis(t *testing.T) {
	var baseline mcpEnvironmentBaseline
	if _, err := baseline.Validate([]string{"metis"}, ""); err == nil || !strings.Contains(err.Error(), "isolated raw") {
		t.Fatalf("preconfigured Metis error=%v", err)
	}
}

func TestAgentCommandEnvironmentPreservesLowercaseProxyRoute(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		t.Setenv(name, "")
	}
	t.Setenv("http_proxy", "http://127.0.0.1:7897")
	t.Setenv("https_proxy", "http://127.0.0.1:7897")
	t.Setenv("all_proxy", "socks5://127.0.0.1:7897")
	t.Setenv("no_proxy", "127.0.0.1,localhost")

	environment := agentCommandEnvironment(s2sbench.AgentRequest{}, t.TempDir())
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	for lower, upper := range map[string]string{"http_proxy": "HTTP_PROXY", "https_proxy": "HTTPS_PROXY", "all_proxy": "ALL_PROXY", "no_proxy": "NO_PROXY"} {
		if values[lower] == "" || values[upper] != values[lower] {
			t.Fatalf("proxy environment %s=%q %s=%q", lower, values[lower], upper, values[upper])
		}
	}
}

func TestCodexSessionClassifiesWebSocketFailureAsUnavailable(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-codex-websocket-failure")
	body := `#!/bin/sh
set -eu
IFS= read -r initialize
echo '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}'
IFS= read -r initialized
IFS= read -r mcp_status
echo '{"id":2,"result":{"data":[],"nextCursor":null}}'
IFS= read -r thread_start
echo '{"id":3,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r turn_start
echo '{"id":4,"result":{"turn":{"id":"turn-1"}}}'
echo 'failed to connect to websocket: Connection refused' >&2
while :; do sleep 1; done
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &codexDriver{binary: script, model: "fake-model"}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "question", Budget: s2sbench.FrozenBudget}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = session.Run(ctx, request)
	if !s2sbench.IsAgentUnavailableError(err) || !strings.Contains(err.Error(), "response transport is unavailable") {
		t.Fatalf("Run error=%v, want AgentUnavailable websocket error", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGenericSessionProtocolV2UsesJSONLOverOneProcess(t *testing.T) {
	root := t.TempDir()
	pidLog := filepath.Join(root, "generic-pids")
	script := filepath.Join(root, "fake-generic")
	body := `#!/bin/sh
set -eu
echo $$ >> "$FAKE_GENERIC_PID_LOG"
IFS= read -r first
echo '{"protocol_version":"s2sbench-driver-v2","output":"SELECT 1","tool_calls":0,"context_tokens":5,"output_tokens":1,"transcript":"turn-1"}'
IFS= read -r second
echo '{"protocol_version":"s2sbench-driver-v2","output":"SELECT 2","tool_calls":0,"context_tokens":6,"output_tokens":1,"transcript":"turn-2"}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(script, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "fake-model"})
	if err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "q1", Environment: map[string]string{"FAKE_GENERIC_PID_LOG": pidLog}, Budget: s2sbench.FrozenBudget}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Prompt = "q2"
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
	pids, err := os.ReadFile(pidLog)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Fields(string(pids)); len(lines) != 1 {
		t.Fatalf("wrapper process starts=%d, want 1", len(lines))
	}
}

func TestAgentCommandEnvironmentScopesRequestOverrides(t *testing.T) {
	t.Setenv("S2SBENCH_CALLER_VALUE", "caller")
	t.Setenv("S2SBENCH_MCP_SECRET", "temporary")
	if err := os.Unsetenv("S2SBENCH_MCP_SECRET"); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := environmentMap(agentCommandEnvironment(s2sbench.AgentRequest{}, home))
	if _, leaked := raw["S2SBENCH_CALLER_VALUE"]; leaked {
		t.Fatal("isolated command inherited an unrelated caller variable")
	}
	if _, leaked := raw["S2SBENCH_MCP_SECRET"]; leaked {
		t.Fatal("raw command received the request-scoped MCP secret")
	}
	metis := environmentMap(agentCommandEnvironment(s2sbench.AgentRequest{Environment: map[string]string{
		"S2SBENCH_MCP_SECRET": "request-scoped",
	}}, home))
	if metis["S2SBENCH_MCP_SECRET"] != "request-scoped" {
		t.Fatalf("request override=%q, want request-scoped", metis["S2SBENCH_MCP_SECRET"])
	}
	if got := os.Getenv("S2SBENCH_MCP_SECRET"); got != "" {
		t.Fatalf("request override mutated process environment to %q", got)
	}
	if raw["HOME"] != home {
		t.Fatalf("isolated HOME = %q, want %q", raw["HOME"], home)
	}
}

func TestPrepareIsolatedCodexHomeCopiesOnlyAuthentication(t *testing.T) {
	source := t.TempDir()
	t.Setenv("CODEX_HOME", source)
	if err := os.WriteFile(filepath.Join(source, "auth.json"), []byte(`{"token":"test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte("[plugins.leaked]\nenabled=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	home, err := prepareIsolatedCodexHome()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil {
		t.Fatalf("isolated auth: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("isolated home copied user config: %v", err)
	}
}

func TestCodexTurnUnavailableErrorPreservesBoundedFailureDetail(t *testing.T) {
	err := codexTurnUnavailableError("failed", json.RawMessage(`{"message":"model is not available"}`))
	if !s2sbench.IsAgentUnavailableError(err) || !strings.Contains(err.Error(), "model is not available") {
		t.Fatalf("turn error = %v, want bounded provider detail", err)
	}
	err = codexTurnUnavailableError("failed", nil)
	if !strings.Contains(err.Error(), `codex turn status "failed"`) {
		t.Fatalf("turn error without detail = %v", err)
	}
}

func TestBoundedToolArgumentsRedactsSecretsAndPreservesDiscoveryInput(t *testing.T) {
	got := boundedToolArguments([]byte(`{"search":["fiscal_week"],"authorization":"Bearer hidden","nested":{"api_key":"hidden"}}`))
	if got != `{"authorization":"[REDACTED]","nested":{"api_key":"[REDACTED]"},"search":["fiscal_week"]}` {
		t.Fatalf("argument summary = %s", got)
	}
}

func environmentMap(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	return values
}

func TestCodexMCPArgsFailClosedAndEscapeValues(t *testing.T) {
	if _, err := codexMCPArgs(agentMCP("bad.name", "http://127.0.0.1/mcp", "TOKEN")); err == nil {
		t.Fatal("codexMCPArgs accepted invalid MCP name")
	}

	mcp := agentMCP("metis", "http://127.0.0.1:18080/mcp", "METIS_S2SBENCH_TOKEN")
	mcp.HTTPHeaders = map[string]string{"X-Metis-Project-Id": "finance"}
	args, err := codexMCPArgs(mcp)
	if err != nil {
		t.Fatalf("codexMCPArgs: %v", err)
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		`mcp_servers.metis.url="http://127.0.0.1:18080/mcp"`,
		`mcp_servers.metis.required=true`,
		`mcp_servers.metis.default_tools_approval_mode="approve"`,
		`mcp_servers.metis.bearer_token_env_var="METIS_S2SBENCH_TOKEN"`,
		`mcp_servers.metis.http_headers={"X-Metis-Project-Id" = "finance"}`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args = %q, missing %q", args, want)
		}
	}
}

func TestCodexAppServerArgsDisableBuiltInAppsForBothArms(t *testing.T) {
	raw, err := codexAppServerArgs(nil)
	if err != nil {
		t.Fatalf("raw codex app-server args: %v", err)
	}
	metisMCP := agentMCP("metis", "http://127.0.0.1:18080/mcp", "METIS_S2SBENCH_TOKEN")
	metis, err := codexAppServerArgs(&metisMCP)
	if err != nil {
		t.Fatalf("Metis codex app-server args: %v", err)
	}
	for arm, args := range map[string][]string{"raw": raw, "metis": metis} {
		joined := strings.Join(args, "\n")
		if !strings.Contains(joined, "--disable\napps") {
			t.Fatalf("%s args = %q, want built-in apps disabled", arm, args)
		}
	}
	if strings.Contains(strings.Join(raw, "\n"), "mcp_servers.metis") {
		t.Fatalf("raw args unexpectedly configure Metis: %q", raw)
	}
	if !strings.Contains(strings.Join(metis, "\n"), `mcp_servers.metis.url="http://127.0.0.1:18080/mcp"`) {
		t.Fatalf("Metis args do not configure benchmark server: %q", metis)
	}
}

func agentMCP(name, url, tokenEnv string) s2sbench.AgentMCP {
	return s2sbench.AgentMCP{Name: name, URL: url, BearerTokenEnvVar: tokenEnv}
}
