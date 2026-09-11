package command

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

const genericDriverProtocolVersion = "s2sbench-driver-v2"

type genericDriver struct {
	binary   string
	args     []string
	identity s2sbench.AgentIdentity
}

type genericSession struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	initial s2sbench.AgentRequest
	home    string
	closed  bool
}

func newGenericDriver(binary string, args []string, identity s2sbench.AgentIdentity) (*genericDriver, error) {
	var err error
	binary, err = resolveExecutable(binary, "")
	if err != nil {
		return nil, fmt.Errorf("resolve generic agent wrapper: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	return &genericDriver{binary: binary, args: append([]string(nil), args...), identity: identity}, nil
}

func (d *genericDriver) Identity(context.Context) (s2sbench.AgentIdentity, error) {
	return d.identity, nil
}

type genericMCP struct {
	Name              string            `json:"name"`
	URL               string            `json:"url"`
	BearerTokenEnvVar string            `json:"bearer_token_env_var,omitempty"`
	HTTPHeaders       map[string]string `json:"http_headers,omitempty"`
}

type genericBudget struct {
	ToolCalls   int `json:"max_tool_calls"`
	Attempts    int `json:"max_attempts"`
	Repetitions int `json:"repetitions_per_question"`
}

type genericRequest struct {
	ProtocolVersion string        `json:"protocol_version"`
	Workspace       string        `json:"workspace"`
	Prompt          string        `json:"prompt"`
	MCP             *genericMCP   `json:"mcp,omitempty"`
	Budget          genericBudget `json:"budget"`
	TurnTimeoutMS   int64         `json:"turn_timeout_ms,omitempty"`
}

type genericResult struct {
	ProtocolVersion string                      `json:"protocol_version"`
	Output          string                      `json:"output"`
	Error           string                      `json:"error,omitempty"`
	ErrorKind       string                      `json:"error_kind,omitempty"`
	ToolCalls       int                         `json:"tool_calls"`
	ContextTokens   int                         `json:"context_tokens"`
	OutputTokens    int                         `json:"output_tokens"`
	Transcript      string                      `json:"transcript"`
	ToolTrace       []s2sbench.ToolCallEvidence `json:"tool_trace,omitempty"`
}

func (d *genericDriver) Open(ctx context.Context, request s2sbench.AgentRequest) (s2sbench.AgentSession, error) {
	if err := validateAgentRequest(request); err != nil {
		return nil, err
	}
	isolatedHome, err := prepareIsolatedAgentHome("s2sbench-generic-home-")
	if err != nil {
		return nil, fmt.Errorf("create isolated generic Agent home: %w", err)
	}
	keepHome := false
	defer func() {
		if !keepHome {
			_ = os.RemoveAll(isolatedHome)
		}
	}()
	// The wrapper owns graceful turn cancellation and must be able to emit its
	// final structured timeout result. Binding its process lifetime to this
	// question context races that result at the exact deadline and discards the
	// already-observed tool trace. CloseSession still always reaps the wrapper.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), d.binary, d.args...)
	cmd.Dir = request.Workspace
	cmd.Env = agentCommandEnvironment(request, isolatedHome)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("generic agent stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("generic agent stdout: %w", err)
	}
	s := &genericSession{cmd: cmd, stdin: stdin, initial: request, home: isolatedHome}
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		return nil, s2sbench.NewAgentUnavailableError(fmt.Errorf("start generic agent wrapper: %w", err))
	}
	s.scanner = bufio.NewScanner(stdout)
	s.scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	keepHome = true
	return s, nil
}

func (s *genericSession) Run(ctx context.Context, request s2sbench.AgentRequest) (s2sbench.AgentResult, error) {
	if s.closed {
		return s2sbench.AgentResult{}, fmt.Errorf("generic agent session is closed")
	}
	if err := validateSessionRequest(s.initial, request); err != nil {
		return s2sbench.AgentResult{}, err
	}
	wireRequest := genericRequest{
		ProtocolVersion: genericDriverProtocolVersion,
		Workspace:       request.Workspace,
		Prompt:          request.Prompt,
		Budget: genericBudget{
			ToolCalls: request.Budget.ToolCalls,
			Attempts:  request.Budget.Attempts,
			// Collection owns independent repetitions and opens a fresh session
			// for each one. A wrapper executes exactly this single turn.
			Repetitions: 1,
		},
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return s2sbench.AgentResult{}, s2sbench.NewAgentTurnTimeoutError("question wall-clock budget is exhausted")
		}
		wireRequest.TurnTimeoutMS = max(1, remaining.Milliseconds())
	}
	if request.MCP != nil {
		wireRequest.MCP = &genericMCP{
			Name:              request.MCP.Name,
			URL:               request.MCP.URL,
			BearerTokenEnvVar: request.MCP.BearerTokenEnvVar,
			HTTPHeaders:       request.MCP.HTTPHeaders,
		}
	}
	body, err := json.Marshal(wireRequest)
	if err != nil {
		return s2sbench.AgentResult{}, fmt.Errorf("encode generic agent request: %w", err)
	}

	if _, err := s.stdin.Write(append(body, '\n')); err != nil {
		return s2sbench.AgentResult{}, s2sbench.NewAgentUnavailableError(fmt.Errorf("write generic agent turn: %w", err))
	}
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return s2sbench.AgentResult{}, fmt.Errorf("read generic agent result: %w", err)
		}
		return s2sbench.AgentResult{}, s2sbench.NewAgentUnavailableError(fmt.Errorf("generic agent wrapper exited before returning a turn: %s", strings.TrimSpace(s.stderr.String())))
	}
	decoder := json.NewDecoder(bytes.NewReader(s.scanner.Bytes()))
	decoder.DisallowUnknownFields()
	var wire genericResult
	if err := decoder.Decode(&wire); err != nil {
		return s2sbench.AgentResult{}, fmt.Errorf("decode generic agent result: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return s2sbench.AgentResult{}, fmt.Errorf("generic agent wrapper returned more than one JSON value")
		}
		return s2sbench.AgentResult{}, fmt.Errorf("decode trailing generic agent output: %w", err)
	}
	if wire.ProtocolVersion != genericDriverProtocolVersion {
		return s2sbench.AgentResult{}, fmt.Errorf("generic agent protocol %q, want %q", wire.ProtocolVersion, genericDriverProtocolVersion)
	}
	if strings.TrimSpace(wire.Transcript) == "" {
		return s2sbench.AgentResult{}, fmt.Errorf("generic agent wrapper must return an auditable transcript")
	}
	result := s2sbench.AgentResult{
		Output:        strings.TrimSpace(wire.Output),
		ToolCalls:     wire.ToolCalls,
		ContextTokens: wire.ContextTokens,
		OutputTokens:  wire.OutputTokens,
		Transcript:    []byte(wire.Transcript),
		ToolTrace:     append([]s2sbench.ToolCallEvidence(nil), wire.ToolTrace...),
	}
	if result.ToolCalls < 0 {
		return result, fmt.Errorf("generic agent reported negative tool calls: %d", result.ToolCalls)
	}
	if result.ToolCalls > request.Budget.ToolCalls {
		// Structured Agent streams can already have multiple tool-start events in a
		// buffered stdout chunk when the first terminal overrun is observed. Freeze
		// normalized accounting at the first over-budget call; the raw transcript
		// remains available for diagnostics without making artifact validity depend
		// on process-signal timing.
		result.ToolCalls = request.Budget.ToolCalls + 1
		if len(result.ToolTrace) > result.ToolCalls {
			result.ToolTrace = result.ToolTrace[:result.ToolCalls]
		}
		return result, s2sbench.NewToolBudgetExceededError(result.ToolCalls, request.Budget.ToolCalls)
	}
	if result.ContextTokens < 0 || result.OutputTokens < 0 {
		return result, fmt.Errorf("generic agent reported negative token usage")
	}
	if message := strings.TrimSpace(wire.Error); message != "" {
		if wire.ErrorKind == "timeout" {
			return result, s2sbench.NewAgentTurnTimeoutError(message)
		}
		return result, fmt.Errorf("generic agent turn failed: %s", message)
	}
	if result.Output == "" {
		return result, fmt.Errorf("generic agent wrapper returned an empty final answer")
	}
	return result, nil
}

func (s *genericSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	_ = s.stdin.Close()
	defer os.RemoveAll(s.home)
	if err := s.cmd.Wait(); err != nil {
		message := strings.TrimSpace(s.stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("generic agent wrapper failed: %s", message)
	}
	return nil
}
