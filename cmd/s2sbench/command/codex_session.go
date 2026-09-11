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
	"path/filepath"
	"strings"
	"sync"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

type codexAppSession struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	events  chan []byte
	stderr  bytes.Buffer
	initial s2sbench.AgentRequest
	thread  string
	nextID  int
	closed  bool
	mu      sync.Mutex
	home    string
}

type codexRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (d *codexDriver) Open(ctx context.Context, request s2sbench.AgentRequest) (s2sbench.AgentSession, error) {
	if err := validateAgentRequest(request); err != nil {
		return nil, err
	}
	if len(request.OutputSchema) != 0 {
		return nil, fmt.Errorf("codex output schema is not used by the SQL benchmark protocol")
	}
	args, err := codexAppServerArgs(request.MCP)
	if err != nil {
		return nil, err
	}
	isolatedHome, err := prepareIsolatedCodexHome()
	if err != nil {
		return nil, err
	}
	keepHome := false
	defer func() {
		if !keepHome {
			_ = os.RemoveAll(isolatedHome)
		}
	}()
	cmd := exec.CommandContext(ctx, d.binary, args...)
	cmd.Dir = request.Workspace
	cmd.Env = environmentWithOverrides(agentCommandEnvironment(request, isolatedHome), map[string]string{"CODEX_HOME": isolatedHome})
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdout: %w", err)
	}
	s := &codexAppSession{cmd: cmd, stdin: stdin, events: make(chan []byte, 64), initial: request, home: isolatedHome}
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		return nil, s2sbench.NewAgentUnavailableError(fmt.Errorf("start codex app-server: %w", err))
	}
	go s.read(stdout)
	if _, err := s.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "metis-s2sbench", "version": "1"}}); err != nil {
		_ = s.abort()
		return nil, err
	}
	if err := s.notify("initialized", map[string]any{}); err != nil {
		_ = s.abort()
		return nil, err
	}
	if err := s.verifyMCPEnvironment(ctx, &d.mcpBaseline, request.MCP); err != nil {
		_ = s.abort()
		return nil, err
	}
	result, err := s.call(ctx, "thread/start", map[string]any{
		"cwd": request.Workspace, "model": d.model, "approvalPolicy": "never",
		"sandbox": "read-only", "ephemeral": true,
	})
	if err != nil {
		_ = s.abort()
		return nil, err
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &started); err != nil || strings.TrimSpace(started.Thread.ID) == "" {
		_ = s.abort()
		return nil, fmt.Errorf("decode codex thread/start response: %w", err)
	}
	s.thread = started.Thread.ID
	keepHome = true
	return s, nil
}

func prepareIsolatedCodexHome() (string, error) {
	home, err := prepareIsolatedAgentHome("s2sbench-codex-home-")
	if err != nil {
		return "", fmt.Errorf("create isolated Codex home: %w", err)
	}
	cleanup := func(err error) (string, error) {
		_ = os.RemoveAll(home)
		return "", err
	}
	sourceHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if sourceHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return cleanup(fmt.Errorf("resolve Codex credential home: %w", err))
		}
		sourceHome = filepath.Join(userHome, ".codex")
	}
	auth, err := os.ReadFile(filepath.Join(sourceHome, "auth.json"))
	switch {
	case err == nil:
		if err := os.WriteFile(filepath.Join(home, "auth.json"), auth, 0o600); err != nil {
			return cleanup(fmt.Errorf("copy Codex authentication into isolated home: %w", err))
		}
	case !os.IsNotExist(err):
		return cleanup(fmt.Errorf("read Codex authentication for isolated home: %w", err))
	}
	return home, nil
}

func (s *codexAppSession) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		s.events <- append([]byte(nil), scanner.Bytes()...)
	}
	close(s.events)
}

func (s *codexAppSession) verifyMCPEnvironment(ctx context.Context, baseline *mcpEnvironmentBaseline, expected *s2sbench.AgentMCP) error {
	result, err := s.call(ctx, "mcpServerStatus/list", map[string]any{})
	if err != nil {
		return fmt.Errorf("verify codex MCP environment: %w", err)
	}
	var status struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result, &status); err != nil {
		return fmt.Errorf("decode codex MCP status: %w", err)
	}
	got := make([]string, 0, len(status.Data))
	for _, server := range status.Data {
		got = append(got, server.Name)
	}
	added := ""
	if expected != nil {
		added = expected.Name
	}
	if _, err := baseline.Validate(got, added); err != nil {
		return fmt.Errorf("codex MCP environment: %w", err)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *codexAppSession) send(value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.stdin.Write(append(body, '\n'))
	return err
}

func (s *codexAppSession) notify(method string, params any) error {
	return s.send(map[string]any{"method": method, "params": params})
}

func (s *codexAppSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.nextID++
	id := s.nextID
	if err := s.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		line, err := s.next(ctx)
		if err != nil {
			return nil, err
		}
		var message codexRPCMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, fmt.Errorf("decode codex app-server message: %w", err)
		}
		if len(message.ID) == 0 {
			continue
		}
		var got int
		if json.Unmarshal(message.ID, &got) != nil || got != id {
			return nil, fmt.Errorf("codex app-server returned unexpected response id %s", message.ID)
		}
		if message.Error != nil {
			return nil, fmt.Errorf("codex app-server %s failed (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	}
}

func (s *codexAppSession) next(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case line, ok := <-s.events:
		if !ok {
			message := strings.TrimSpace(s.stderr.String())
			if message == "" {
				message = "stdout closed"
			}
			return nil, s2sbench.NewAgentUnavailableError(fmt.Errorf("codex app-server exited: %s", message))
		}
		return line, nil
	}
}

func (s *codexAppSession) Run(ctx context.Context, request s2sbench.AgentRequest) (s2sbench.AgentResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s2sbench.AgentResult{}, fmt.Errorf("codex session is closed")
	}
	if err := validateSessionRequest(s.initial, request); err != nil {
		return s2sbench.AgentResult{}, err
	}
	s.nextID++
	id := s.nextID
	turnRequest := map[string]any{"id": id, "method": "turn/start", "params": map[string]any{
		"threadId": s.thread, "input": []map[string]string{{"type": "text", "text": request.Prompt}},
	}}
	if err := s.send(turnRequest); err != nil {
		return s2sbench.AgentResult{}, err
	}
	var result s2sbench.AgentResult
	var transcript bytes.Buffer
	seen := map[string]struct{}{}
	turnID := ""
	responseSeen := false
	var budgetErr error
	for {
		line, err := s.next(ctx)
		if err != nil {
			result.Transcript = append([]byte(nil), transcript.Bytes()...)
			if budgetErr != nil {
				s.closed = true
				_ = s.abort()
				return result, budgetErr
			}
			if errors.Is(err, context.DeadlineExceeded) {
				s.closed = true
				_ = s.abort()
				if unavailable := codexResponseTransportUnavailable(s.stderr.String()); unavailable != nil {
					return result, s2sbench.NewAgentUnavailableError(unavailable)
				}
			}
			return result, err
		}
		transcript.Write(line)
		transcript.WriteByte('\n')
		var message codexRPCMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return result, fmt.Errorf("decode codex app-server turn event: %w", err)
		}
		if len(message.ID) != 0 {
			var got int
			if json.Unmarshal(message.ID, &got) != nil || got != id {
				return result, fmt.Errorf("unexpected codex response id %s during turn", message.ID)
			}
			if message.Error != nil {
				return result, fmt.Errorf("codex turn/start failed: %s", message.Error.Message)
			}
			var response struct {
				Turn struct {
					ID string `json:"id"`
				} `json:"turn"`
			}
			if err := json.Unmarshal(message.Result, &response); err != nil {
				return result, err
			}
			turnID, responseSeen = response.Turn.ID, true
			continue
		}
		switch message.Method {
		case "thread/tokenUsage/updated":
			var p struct {
				TurnID     string `json:"turnId"`
				TokenUsage struct {
					Last struct {
						Input  int `json:"inputTokens"`
						Output int `json:"outputTokens"`
					} `json:"last"`
				} `json:"tokenUsage"`
			}
			if json.Unmarshal(message.Params, &p) == nil && (turnID == "" || p.TurnID == turnID) {
				result.ContextTokens, result.OutputTokens = p.TokenUsage.Last.Input, p.TokenUsage.Last.Output
			}
		case "item/completed":
			var p struct {
				TurnID string `json:"turnId"`
				Item   struct {
					ID, Type, Text, Server, Tool, Status string
					Arguments, Result, Error             json.RawMessage
					DurationMS                           int64 `json:"durationMs"`
				} `json:"item"`
			}
			if err := json.Unmarshal(message.Params, &p); err != nil {
				return result, err
			}
			if turnID != "" && p.TurnID != turnID {
				continue
			}
			if p.Item.Type == "agentMessage" {
				result.Output = strings.TrimSpace(p.Item.Text)
				continue
			}
			if codexAppTool(p.Item.Type) {
				if _, ok := seen[p.Item.ID]; !ok {
					seen[p.Item.ID] = struct{}{}
					result.ToolCalls++
				}
				if result.ToolCalls > request.Budget.ToolCalls && budgetErr == nil {
					_ = s.notify("turn/interrupt", map[string]string{"threadId": s.thread, "turnId": turnID})
					budgetErr = s2sbench.NewToolBudgetExceededError(result.ToolCalls, request.Budget.ToolCalls)
				}
				if p.Item.Type == "mcpToolCall" {
					if p.Item.Server == benchmarkMCPServerName {
						if request.MCP == nil {
							return result, fmt.Errorf("codex used benchmark Metis MCP in the raw-assets arm")
						}
						status := "success"
						if p.Item.Status == "failed" || hasJSONValue(p.Item.Error) {
							status = "error"
						} else if p.Item.Status != "completed" && p.Item.Status != "success" {
							status = "incomplete"
						}
						result.ToolTrace = append(result.ToolTrace, s2sbench.ToolCallEvidence{Name: p.Item.Server + "." + p.Item.Tool, DurationMS: p.Item.DurationMS, RequestBytes: len(p.Item.Arguments), ResponseBytes: len(p.Item.Result) + len(p.Item.Error), Arguments: boundedToolArguments(p.Item.Arguments), Response: boundedSemanticToolResponse(p.Item.Tool, p.Item.Result), Status: status, Error: toolErrorSummary(p.Item.Error)})
					}
				}
				if budgetErr != nil {
					result.Transcript = append([]byte(nil), transcript.Bytes()...)
					return result, budgetErr
				}
			}
		case "turn/completed":
			if !responseSeen {
				return result, fmt.Errorf("codex completed a turn before turn/start response")
			}
			var p struct {
				Turn struct {
					ID, Status string
					Error      json.RawMessage `json:"error"`
				} `json:"turn"`
			}
			if err := json.Unmarshal(message.Params, &p); err != nil {
				return result, err
			}
			if p.Turn.ID != turnID {
				continue
			}
			result.Transcript = append([]byte(nil), transcript.Bytes()...)
			if budgetErr != nil {
				return result, budgetErr
			}
			if p.Turn.Status != "completed" {
				return result, codexTurnUnavailableError(p.Turn.Status, p.Turn.Error)
			}
			if result.Output == "" {
				return result, fmt.Errorf("codex returned an empty final message")
			}
			return result, nil
		}
	}
}

func codexTurnUnavailableError(status string, raw json.RawMessage) error {
	message := toolErrorSummary(raw)
	if message == "" {
		return s2sbench.NewAgentUnavailableError(fmt.Errorf("codex turn status %q", status))
	}
	return s2sbench.NewAgentUnavailableError(fmt.Errorf("codex turn status %q: %s", status, message))
}

func hasJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func toolErrorSummary(value json.RawMessage) string {
	if !hasJSONValue(value) {
		return ""
	}
	var message string
	if json.Unmarshal(value, &message) != nil {
		var compact bytes.Buffer
		if json.Compact(&compact, value) == nil {
			message = compact.String()
		} else {
			message = string(bytes.TrimSpace(value))
		}
	}
	message = strings.TrimSpace(message)
	if len(message) > 4096 {
		message = message[:4096]
	}
	return message
}

func boundedSemanticToolResponse(tool string, value json.RawMessage) string {
	semanticResult := tool == "compile_sql" || tool == "query_metrics" || strings.HasSuffix(tool, ".compile_sql") || strings.HasSuffix(tool, ".query_metrics") || strings.HasSuffix(tool, "_compile_sql") || strings.HasSuffix(tool, "_query_metrics")
	if !semanticResult || !hasJSONValue(value) || len(value) > 16*1024*1024 {
		return ""
	}
	return string(bytes.TrimSpace(value))
}

func codexResponseTransportUnavailable(stderr string) error {
	message := strings.TrimSpace(stderr)
	if message == "" {
		return nil
	}
	lower := strings.ToLower(message)
	for _, marker := range []string{
		"failed to connect to websocket",
		"responses_websocket",
		"connection refused",
		"network is unreachable",
		"failed to lookup address information",
		"dns error",
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("codex response transport is unavailable: %s", message)
		}
	}
	return nil
}

func codexAppTool(kind string) bool {
	switch kind {
	case "commandExecution", "fileChange", "mcpToolCall", "webSearch", "collabAgentToolCall", "dynamicToolCall":
		return true
	default:
		return false
	}
}

func (s *codexAppSession) abort() error {
	defer os.RemoveAll(s.home)
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		return s.cmd.Wait()
	}
	return nil
}

func (s *codexAppSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	defer os.RemoveAll(s.home)
	_ = s.stdin.Close()
	if err := s.cmd.Wait(); err != nil {
		message := strings.TrimSpace(s.stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("codex app-server failed: %s", message)
	}
	return nil
}
