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

func TestGenericSessionPassesRemainingQuestionDeadlineToWrapper(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-generic-deadline")
	body := `#!/bin/sh
set -eu
IFS= read -r request
printf '%s' "$request" > request.json
echo '{"protocol_version":"s2sbench-driver-v2","output":"SELECT 1","tool_calls":0,"context_tokens":1,"output_tokens":1,"transcript":"turn-1"}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(script, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "fake-model"})
	if err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "q", Budget: s2sbench.FrozenBudget}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := driver.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	bodyBytes, err := os.ReadFile(filepath.Join(root, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wire genericRequest
	if err := json.Unmarshal(bodyBytes, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.TurnTimeoutMS <= 0 || wire.TurnTimeoutMS > 2000 {
		t.Fatalf("turn timeout = %dms", wire.TurnTimeoutMS)
	}
}

func TestGenericSessionPreservesStructuredToolTrace(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-generic-trace")
	body := `#!/bin/sh
set -eu
IFS= read -r request
echo '{"protocol_version":"s2sbench-driver-v2","output":"SELECT 1","tool_calls":1,"context_tokens":5,"output_tokens":1,"transcript":"turn-1","tool_trace":[{"name":"metis.list_metrics","duration_ms":4,"request_bytes":12,"response_bytes":24,"status":"success"}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(script, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "fake-model"})
	if err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "q", Budget: s2sbench.FrozenBudget}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if len(result.ToolTrace) != 1 || result.ToolTrace[0].Name != "metis.list_metrics" || result.ToolTrace[0].Status != "success" {
		t.Fatalf("tool trace = %#v", result.ToolTrace)
	}
}

func TestGenericDriverResolvesRelativeWrapperBeforeWorkspaceChange(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "driver.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(root)
	t.Chdir(parent)
	relative, err := filepath.Rel(parent, script)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(relative, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if driver.binary != script {
		t.Fatalf("driver binary = %q, want absolute %q", driver.binary, script)
	}
}

func TestGenericSessionNormalizesBufferedTerminalToolOverrun(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-generic-overrun")
	body := `#!/bin/sh
set -eu
IFS= read -r request
echo '{"protocol_version":"s2sbench-driver-v2","output":"budget exceeded","tool_calls":32,"context_tokens":5,"output_tokens":1,"transcript":"observed 32 buffered starts","tool_trace":[{"name":"metis.list_metrics","duration_ms":0,"request_bytes":0,"response_bytes":0,"status":"success"}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(script, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "fake-model"})
	if err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "q", Budget: s2sbench.FrozenBudget}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := session.Run(context.Background(), request)
	if closeErr := session.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	var exceeded *s2sbench.ToolBudgetExceededError
	if !errors.As(runErr, &exceeded) {
		t.Fatalf("Run error = %v, want ToolBudgetExceededError", runErr)
	}
	want := request.Budget.ToolCalls + 1
	if result.ToolCalls != want || exceeded.Used != want || exceeded.Budget != request.Budget.ToolCalls {
		t.Fatalf("normalized overrun result=%d error=%+v want=%d/%d", result.ToolCalls, exceeded, want, request.Budget.ToolCalls)
	}
	if len(result.ToolTrace) != 1 || result.ToolTrace[0].Name != "metis.list_metrics" {
		t.Fatalf("tool trace = %#v", result.ToolTrace)
	}
}

func TestGenericSessionReturnsStructuredWrapperFailure(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-generic-failure")
	body := `#!/bin/sh
set -eu
IFS= read -r request
echo '{"protocol_version":"s2sbench-driver-v2","output":"","error":"Pi exceeded turn timeout 60000ms","error_kind":"timeout","tool_calls":1,"context_tokens":5,"output_tokens":1,"transcript":"turn-timeout","tool_trace":[{"name":"bash","duration_ms":0,"request_bytes":12,"response_bytes":0,"status":"incomplete"}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(script, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "fake-model"})
	if err != nil {
		t.Fatal(err)
	}
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "q", Budget: s2sbench.FrozenBudget}
	session, err := driver.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := session.Run(context.Background(), request)
	if closeErr := session.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr == nil || !strings.Contains(runErr.Error(), "Pi exceeded turn timeout 60000ms") {
		t.Fatalf("Run error = %v, want structured timeout", runErr)
	}
	if !s2sbench.IsAgentTurnTimeoutError(runErr) {
		t.Fatalf("Run error type = %T, want AgentTurnTimeoutError", runErr)
	}
	if result.Output != "" || result.ToolCalls != 1 || len(result.ToolTrace) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestGenericSessionSurvivesQuestionDeadlineLongEnoughToReturnTimeoutEvidence(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-generic-timeout-evidence")
	body := `#!/bin/sh
set -eu
IFS= read -r request
sleep 0.03
echo '{"protocol_version":"s2sbench-driver-v2","output":"","error":"agent timed out","error_kind":"timeout","tool_calls":3,"context_tokens":17,"output_tokens":2,"transcript":"partial turn","tool_trace":[{"name":"metis.list_metrics","duration_ms":1,"status":"success"}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := newGenericDriver(script, nil, s2sbench.AgentIdentity{Name: "fake", Version: "1", Model: "fake-model"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request := s2sbench.AgentRequest{Workspace: root, Prompt: "q", Budget: s2sbench.FrozenBudget}
	session, err := driver.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := session.Run(ctx, request)
	if closeErr := session.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !s2sbench.IsAgentTurnTimeoutError(runErr) {
		t.Fatalf("Run error = %v, want structured timeout", runErr)
	}
	if result.ToolCalls != 3 || result.ContextTokens != 17 || result.OutputTokens != 2 || len(result.ToolTrace) != 1 {
		t.Fatalf("partial timeout evidence = %#v", result)
	}
}
