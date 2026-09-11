package command

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

type claudeCodeDriver struct {
	binary      string
	model       string
	mcpBaseline mcpEnvironmentBaseline
}

type claudeCodeSession struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	scanner     *bufio.Scanner
	stderr      bytes.Buffer
	request     s2sbench.AgentRequest
	model       string
	expectedMCP string
	driver      *claudeCodeDriver
	allowedMCP  []string
	mcpPath     string
	home        string
	stopped     bool
	closed      bool
}

func newClaudeCodeDriver(binary, model string) (*claudeCodeDriver, error) {
	binary = strings.TrimSpace(binary)
	model = strings.TrimSpace(model)
	if binary == "" {
		binary = "claude"
	}
	if model == "" {
		return nil, fmt.Errorf("Claude Code model is required")
	}
	return &claudeCodeDriver{binary: binary, model: model}, nil
}

func (d *claudeCodeDriver) Identity(ctx context.Context) (s2sbench.AgentIdentity, error) {
	version, err := commandVersion(ctx, d.binary, "--version")
	if err != nil {
		return s2sbench.AgentIdentity{}, fmt.Errorf("Claude Code version: %w", err)
	}
	identity := s2sbench.AgentIdentity{Name: "claude-code", Version: version, Model: d.model}
	if err := identity.Validate(); err != nil {
		return s2sbench.AgentIdentity{}, err
	}
	return identity, nil
}

func (d *claudeCodeDriver) Open(ctx context.Context, request s2sbench.AgentRequest) (s2sbench.AgentSession, error) {
	if err := validateAgentRequest(request); err != nil {
		return nil, err
	}
	if len(request.OutputSchema) != 0 {
		return nil, fmt.Errorf("Claude Code output schema is not used by the SQL benchmark protocol")
	}

	mcpPath, expectedMCP, err := writeClaudeMCPConfig(request.MCP)
	if err != nil {
		return nil, err
	}
	keepMCPConfig := false
	defer func() {
		if !keepMCPConfig {
			_ = os.Remove(mcpPath)
		}
	}()
	args := claudeExecArgs(d.model, mcpPath, expectedMCP)
	isolatedHome, err := prepareIsolatedAgentHome("s2sbench-claude-home-")
	if err != nil {
		return nil, fmt.Errorf("create isolated Claude home: %w", err)
	}
	keepHome := false
	defer func() {
		if !keepHome {
			_ = os.RemoveAll(isolatedHome)
		}
	}()
	environment, err := claudeCommandEnvironment(request, isolatedHome)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, d.binary, args...)
	cmd.Dir = request.Workspace
	cmd.Env = environment
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Claude Code stdout: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("Claude Code stdin: %w", err)
	}
	session := &claudeCodeSession{cmd: cmd, stdin: stdin, request: request, model: d.model, expectedMCP: expectedMCP, mcpPath: mcpPath, home: isolatedHome, driver: d}
	cmd.Stderr = &session.stderr
	if err := cmd.Start(); err != nil {
		return nil, s2sbench.NewAgentUnavailableError(fmt.Errorf("start Claude Code: %w", err))
	}
	session.scanner = bufio.NewScanner(stdout)
	session.scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	keepMCPConfig = true
	keepHome = true
	return session, nil
}

func claudeCommandEnvironment(request s2sbench.AgentRequest, home string) ([]string, error) {
	overrides := map[string]string{}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve Claude credential home: %w", err)
	}
	body, err := os.ReadFile(filepath.Join(userHome, ".claude", "settings.json"))
	if err == nil {
		var settings struct {
			Environment map[string]string `json:"env"`
		}
		if err := json.Unmarshal(body, &settings); err != nil {
			return nil, fmt.Errorf("decode Claude provider environment: %w", err)
		}
		for name, value := range settings.Environment {
			if strings.HasPrefix(name, "ANTHROPIC_") || name == "API_TIMEOUT_MS" || name == "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC" {
				overrides[name] = value
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read Claude provider environment: %w", err)
	}
	for name, value := range request.Environment {
		overrides[name] = value
	}
	baseRequest := request
	baseRequest.Environment = nil
	return environmentWithOverrides(agentCommandEnvironment(baseRequest, home), overrides), nil
}

func (s *claudeCodeSession) Run(ctx context.Context, request s2sbench.AgentRequest) (s2sbench.AgentResult, error) {
	if s.closed {
		return s2sbench.AgentResult{}, fmt.Errorf("Claude Code session is closed")
	}
	if err := validateSessionRequest(s.request, request); err != nil {
		return s2sbench.AgentResult{}, err
	}
	input := struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}{Type: "user"}
	input.Message.Role = "user"
	input.Message.Content = request.Prompt
	body, _ := json.Marshal(input)
	if _, err := s.stdin.Write(append(body, '\n')); err != nil {
		return s2sbench.AgentResult{}, s2sbench.NewAgentUnavailableError(fmt.Errorf("write Claude Code turn: %w", err))
	}
	parsed, err := parseClaudeTurn(s.scanner, request.Budget.ToolCalls, s.model, s.expectedMCP, &s.driver.mcpBaseline, s.allowedMCP, func() {
		s.stopped = true
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
	if parsed.AllowedMCP != nil {
		s.allowedMCP = append([]string(nil), parsed.AllowedMCP...)
	}
	if err != nil {
		return parsed.Result, err
	}
	if !parsed.InitSeen {
		return parsed.Result, s2sbench.NewAgentUnavailableError(fmt.Errorf("Claude Code stream contained no system init event"))
	}
	if !parsed.ResultSeen {
		return parsed.Result, fmt.Errorf("Claude Code stream contained no final result event")
	}
	if parsed.Subtype != "success" || parsed.IsError {
		return parsed.Result, s2sbench.NewAgentUnavailableError(fmt.Errorf("Claude Code result subtype %q error=%v", parsed.Subtype, parsed.IsError))
	}
	if strings.TrimSpace(parsed.Result.Output) == "" {
		return parsed.Result, fmt.Errorf("Claude Code returned an empty final result")
	}
	return parsed.Result, nil
}

func (s *claudeCodeSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	_ = s.stdin.Close()
	defer os.Remove(s.mcpPath)
	defer os.RemoveAll(s.home)
	if err := s.cmd.Wait(); err != nil {
		if s.stopped {
			return nil
		}
		message := strings.TrimSpace(s.stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Claude Code failed: %s", message)
	}
	return nil
}

func claudeExecArgs(model, mcpPath, _ string) []string {
	// Keep the Claude Code harness identical across both arms. A fresh isolated
	// HOME and asset-only workspace remove user/project customizations; strict MCP
	// config leaves path B's benchmark-owned Metis server as the only experimental
	// delta. Claude's safe mode cannot be used because it also disables MCP servers.
	// bypassPermissions prevents an unattended run from stopping for permission
	// input. Both arms keep the same read-only built-in tool surface, and strict
	// MCP configuration exposes only the benchmark server. Claude discovers that
	// server's tools itself, so the harness never freezes a tool-name allowlist.
	// The frozen budget is enforced from structured tool_use events.
	tools := []string{"Read", "Grep", "Glob"}
	args := []string{
		"-p",
		"--strict-mcp-config",
		"--no-chrome",
		"--disable-slash-commands",
		"--tools", strings.Join(tools, ","),
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--model", model,
		"--no-session-persistence",
		"--mcp-config", mcpPath,
	}
	args = append(args, "--permission-mode", "bypassPermissions")
	return args
}

func writeClaudeMCPConfig(mcp *s2sbench.AgentMCP) (string, string, error) {
	config := struct {
		Servers map[string]claudeMCPServer `json:"mcpServers"`
	}{Servers: map[string]claudeMCPServer{}}
	expectedName := ""
	if mcp != nil {
		expectedName = strings.TrimSpace(mcp.Name)
		if !agentNamePattern.MatchString(expectedName) {
			return "", "", fmt.Errorf("invalid MCP name %q", mcp.Name)
		}
		if strings.TrimSpace(mcp.URL) == "" {
			return "", "", fmt.Errorf("MCP URL is required")
		}
		server := claudeMCPServer{Type: "http", URL: strings.TrimSpace(mcp.URL)}
		if len(mcp.HTTPHeaders) > 0 {
			server.Headers = make(map[string]string, len(mcp.HTTPHeaders)+1)
			for name, value := range mcp.HTTPHeaders {
				server.Headers[name] = value
			}
		}
		if env := strings.TrimSpace(mcp.BearerTokenEnvVar); env != "" {
			if server.Headers == nil {
				server.Headers = map[string]string{}
			}
			server.Headers["Authorization"] = "Bearer ${" + env + "}"
		}
		config.Servers[expectedName] = server
	}
	body, err := json.Marshal(config)
	if err != nil {
		return "", "", fmt.Errorf("encode Claude MCP config: %w", err)
	}
	file, err := os.CreateTemp("", "s2sbench-claude-mcp-*.json")
	if err != nil {
		return "", "", fmt.Errorf("create Claude MCP config: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return "", "", fmt.Errorf("chmod Claude MCP config: %w", err)
	}
	if _, err := file.Write(body); err != nil {
		cleanup()
		return "", "", fmt.Errorf("write Claude MCP config: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", "", fmt.Errorf("close Claude MCP config: %w", err)
	}
	return path, expectedName, nil
}

type claudeMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type claudeWireMessage struct {
	ID      string               `json:"id"`
	Content []claudeContentBlock `json:"content"`
	Usage   *claudeUsage         `json:"usage"`
}

type claudeMCPStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type claudeEvent struct {
	Type            string             `json:"type"`
	Subtype         string             `json:"subtype"`
	ParentToolUseID *string            `json:"parent_tool_use_id"`
	Message         *claudeWireMessage `json:"message"`
	Model           string             `json:"model"`
	MCPServers      []claudeMCPStatus  `json:"mcp_servers"`
	Result          string             `json:"result"`
	IsError         bool               `json:"is_error"`
	Usage           *claudeUsage       `json:"usage"`
}

type claudeParsedStream struct {
	Result     s2sbench.AgentResult
	InitSeen   bool
	ResultSeen bool
	Subtype    string
	IsError    bool
	AllowedMCP []string
}

// parseClaudeStream uses the same tool accounting for both arms. Built-in
// built-in tools and subagents are normal Agent behavior. User plugins, skills,
// hooks, and MCP servers are disabled; path B adds only benchmark Metis.
func parseClaudeStream(r io.Reader, toolBudget int, expectedModel, expectedMCP string, stop func()) (claudeParsedStream, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	baseline := &mcpEnvironmentBaseline{}
	return parseClaudeTurn(scanner, toolBudget, expectedModel, expectedMCP, baseline, nil, stop)
}

func parseClaudeTurn(scanner *bufio.Scanner, toolBudget int, expectedModel, expectedMCP string, baseline *mcpEnvironmentBaseline, initialAllowedMCP []string, stop func()) (claudeParsedStream, error) {
	var parsed claudeParsedStream
	if toolBudget < 0 {
		return parsed, fmt.Errorf("negative tool budget")
	}
	var transcript bytes.Buffer
	seenTools := map[string]struct{}{}
	seenUsageMessages := map[string]struct{}{}
	traceByToolID := map[string]int{}
	allowedMCP := append([]string(nil), initialAllowedMCP...)
	if initialAllowedMCP != nil {
		parsed.AllowedMCP = append([]string(nil), initialAllowedMCP...)
	}

	fail := func(err error) (claudeParsedStream, error) {
		if stop != nil {
			stop()
		}
		parsed.Result.Transcript = append([]byte(nil), transcript.Bytes()...)
		return parsed, err
	}

	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		transcript.Write(line)
		transcript.WriteByte('\n')

		var event claudeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return fail(fmt.Errorf("decode Claude stream-json event: %w", err))
		}
		switch event.Type {
		case "system":
			switch event.Subtype {
			case "init":
				if parsed.InitSeen {
					return fail(fmt.Errorf("Claude Code emitted multiple init events"))
				}
				parsed.InitSeen = true
				if strings.TrimSpace(event.Model) != strings.TrimSpace(expectedModel) {
					return fail(fmt.Errorf("Claude Code initialized model %q, want pinned %q", event.Model, expectedModel))
				}
				if expectedMCP != "" {
					status, ok := claudeMCPServerStatus(event.MCPServers, expectedMCP)
					if !ok {
						return fail(fmt.Errorf("Claude Code init did not report required MCP server %q", expectedMCP))
					}
					if status != "connected" {
						return fail(fmt.Errorf("Claude Code MCP server %q status is %q, want connected", expectedMCP, status))
					}
				}
				names := make([]string, 0, len(event.MCPServers))
				for _, server := range event.MCPServers {
					names = append(names, server.Name)
				}
				var err error
				allowedMCP, err = baseline.Validate(names, expectedMCP)
				if err != nil {
					return fail(fmt.Errorf("Claude Code MCP environment: %w", err))
				}
				parsed.AllowedMCP = append([]string(nil), allowedMCP...)
			case "status", "api_retry", "compact_boundary", "thinking_tokens", "local_command_output", "plugin_install", "hook_started", "hook_progress", "hook_response", "files_persisted", "task_started", "task_progress", "task_updated", "task_notification", "permission_denied":
				// Observable harness lifecycle. Provider-internal API retry events
				// remain transcript evidence but are not benchmark answer attempts.
				// Tool invocations are counted from assistant tool_use blocks.
			default:
				return fail(fmt.Errorf("unrecognized Claude Code system subtype %q; evidence accounting would be ambiguous", event.Subtype))
			}
		case "assistant":
			if event.Message == nil {
				return fail(fmt.Errorf("Claude Code assistant event has no message"))
			}
			if event.Message.Usage != nil {
				messageID := strings.TrimSpace(event.Message.ID)
				if _, seen := seenUsageMessages[messageID]; messageID != "" && !seen {
					seenUsageMessages[messageID] = struct{}{}
					usage := *event.Message.Usage
					if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheCreationInputTokens < 0 || usage.CacheReadInputTokens < 0 {
						return fail(fmt.Errorf("Claude Code assistant message reports negative usage"))
					}
					parsed.Result.ContextTokens += usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
					parsed.Result.OutputTokens += usage.OutputTokens
				}
			}
			for _, block := range event.Message.Content {
				switch block.Type {
				case "text", "thinking", "redacted_thinking":
					continue
				case "tool_use":
					id := strings.TrimSpace(block.ID)
					name := strings.TrimSpace(block.Name)
					if id == "" || name == "" {
						return fail(fmt.Errorf("Claude Code tool_use block is missing id or name"))
					}
					if err := validateClaudeToolInterface(name, expectedMCP, allowedMCP); err != nil {
						return fail(err)
					}
					if _, exists := seenTools[id]; !exists {
						seenTools[id] = struct{}{}
						parsed.Result.ToolCalls++
						if server, tool, ok := claudeMCPTool(name); ok && server == benchmarkMCPServerName {
							traceByToolID[id] = len(parsed.Result.ToolTrace)
							parsed.Result.ToolTrace = append(parsed.Result.ToolTrace, s2sbench.ToolCallEvidence{
								Name:         server + "." + tool,
								RequestBytes: len(bytes.TrimSpace(block.Input)),
								Arguments:    boundedToolArguments(bytes.TrimSpace(block.Input)),
								Status:       "incomplete",
							})
						}
						if parsed.Result.ToolCalls > toolBudget {
							return fail(s2sbench.NewToolBudgetExceededError(parsed.Result.ToolCalls, toolBudget))
						}
					}
				default:
					return fail(fmt.Errorf("unrecognized Claude Code assistant content block %q", block.Type))
				}
			}
		case "user":
			// Tool results are user messages in the Claude protocol. Tool uses are
			// counted from the originating assistant tool_use blocks only.
			if event.Message != nil {
				for _, block := range event.Message.Content {
					if block.Type != "tool_result" {
						continue
					}
					index, ok := traceByToolID[strings.TrimSpace(block.ToolUseID)]
					if !ok {
						continue
					}
					parsed.Result.ToolTrace[index].ResponseBytes = len(bytes.TrimSpace(block.Content))
					parsed.Result.ToolTrace[index].Status = "success"
					if block.IsError {
						parsed.Result.ToolTrace[index].Status = "error"
						parsed.Result.ToolTrace[index].Error = toolErrorSummary(block.Content)
					} else {
						parsed.Result.ToolTrace[index].Response = boundedSemanticToolResponse(parsed.Result.ToolTrace[index].Name, block.Content)
					}
				}
			}
		case "tool_progress", "tool_use_summary", "rate_limit_event", "auth_status", "prompt_suggestion":
			// Diagnostics only. The originating tool_use has already been counted.
		case "result":
			if parsed.ResultSeen {
				return fail(fmt.Errorf("Claude Code emitted multiple result events"))
			}
			parsed.ResultSeen = true
			parsed.Subtype = event.Subtype
			parsed.IsError = event.IsError
			if event.Usage == nil {
				return fail(fmt.Errorf("Claude Code result has no usage"))
			}
			usage := *event.Usage
			if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheCreationInputTokens < 0 || usage.CacheReadInputTokens < 0 {
				return fail(fmt.Errorf("Claude Code result reports negative usage"))
			}
			parsed.Result.ContextTokens = usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
			parsed.Result.OutputTokens = usage.OutputTokens
			parsed.Result.Output = strings.TrimSpace(event.Result)
			parsed.Result.Transcript = append([]byte(nil), transcript.Bytes()...)
			return parsed, nil
		case "stream_event":
			return fail(fmt.Errorf("unexpected Claude Code partial stream event; include-partial-messages must remain disabled"))
		default:
			return fail(fmt.Errorf("unrecognized Claude Code event type %q; evidence accounting would be ambiguous", event.Type))
		}
	}
	if err := scanner.Err(); err != nil {
		return fail(fmt.Errorf("read Claude Code stream-json: %w", err))
	}
	parsed.Result.Transcript = append([]byte(nil), transcript.Bytes()...)
	return parsed, nil
}

func claudeMCPServerStatus(servers []claudeMCPStatus, name string) (string, bool) {
	for _, server := range servers {
		if server.Name == name {
			return server.Status, true
		}
	}
	return "", false
}

func validateClaudeToolInterface(name, expectedMCP string, allowedMCP []string) error {
	if !strings.HasPrefix(name, "mcp__") {
		return nil
	}
	serverAndTool := strings.TrimPrefix(name, "mcp__")
	server, _, ok := strings.Cut(serverAndTool, "__")
	if !ok || server == "" {
		return fmt.Errorf("Claude Code used malformed MCP tool %q", name)
	}
	if !containsString(allowedMCP, server) {
		return fmt.Errorf("Claude Code used MCP tool %q outside the isolated benchmark environment", name)
	}
	if server == benchmarkMCPServerName && expectedMCP == "" {
		return fmt.Errorf("Claude Code used benchmark Metis MCP in the raw-assets arm")
	}
	return nil
}

func claudeMCPTool(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, "mcp__") {
		return "", "", false
	}
	serverAndTool := strings.TrimPrefix(name, "mcp__")
	server, tool, ok = strings.Cut(serverAndTool, "__")
	return server, tool, ok && server != "" && tool != ""
}
