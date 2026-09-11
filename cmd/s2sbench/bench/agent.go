package s2sbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// AgentUnavailableError marks a process/provider/model failure that makes
// further benchmark attempts meaningless. It is not a semantic answer and is
// therefore not scored or journaled as a completed repetition; collection
// stops so the same identity can be retried by an explicit resume.
type AgentUnavailableError struct {
	Err error
}

func (e *AgentUnavailableError) Error() string {
	if e == nil || e.Err == nil {
		return "external agent is unavailable"
	}
	return "external agent is unavailable: " + e.Err.Error()
}

func (e *AgentUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewAgentUnavailableError(err error) error {
	if err == nil || IsAgentUnavailableError(err) {
		return err
	}
	return &AgentUnavailableError{Err: err}
}

func IsAgentUnavailableError(err error) bool {
	var target *AgentUnavailableError
	return errors.As(err, &target)
}

// ToolBudgetExceededError is a scored, terminal attempt failure. Drivers emit
// it after observing the first call beyond the frozen limit; the collector must
// journal that evidence before stopping the now-terminated agent session.
type ToolBudgetExceededError struct {
	Used   int
	Budget int
}

func (e *ToolBudgetExceededError) Error() string {
	return fmt.Sprintf("external agent exceeded tool-call budget: used %d, budget %d", e.Used, e.Budget)
}

func NewToolBudgetExceededError(used, budget int) error {
	return &ToolBudgetExceededError{Used: used, Budget: budget}
}

func IsToolBudgetExceededError(err error) bool {
	var target *ToolBudgetExceededError
	return errors.As(err, &target)
}

// AgentTurnTimeoutError is a terminal failure for one Agent turn. Repeating
// the same turn in the same question session after the wrapper deadline only
// doubles latency and cost without providing repair evidence from execution.
type AgentTurnTimeoutError struct {
	Message string
}

func (e *AgentTurnTimeoutError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "external agent turn timed out"
	}
	return "external agent turn timed out: " + strings.TrimSpace(e.Message)
}

func NewAgentTurnTimeoutError(message string) error {
	return &AgentTurnTimeoutError{Message: message}
}

func IsAgentTurnTimeoutError(err error) bool {
	var target *AgentTurnTimeoutError
	return errors.As(err, &target)
}

// AgentIdentity pins the executable agent that owns the model/tool loop for a
// collection. Agent and model identity are separate: changing either changes
// the system under test and therefore requires a separate result set.
type AgentIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Model   string `json:"model"`
}

func (i AgentIdentity) Validate() error {
	if strings.TrimSpace(i.Name) == "" {
		return fmt.Errorf("agent name is required")
	}
	if strings.TrimSpace(i.Version) == "" {
		return fmt.Errorf("agent version is required")
	}
	if strings.TrimSpace(i.Model) == "" {
		return fmt.Errorf("agent model is required")
	}
	return nil
}

// AgentMCP describes the one benchmark-owned MCP endpoint exposed to an agent.
// Authentication stays in an environment variable so generated config files do
// not persist bearer credentials.
type AgentMCP struct {
	Name              string
	URL               string
	BearerTokenEnvVar string
	HTTPHeaders       map[string]string
}

// AgentRequest is turn-level input to a real local agent. Workspace is the
// arm-isolated session directory containing only the assets that arm may see.
// Prompt is the frozen benchmark protocol plus the frozen business question.
// MCP is nil for raw-assets and points at Metis for the Metis arm.
type AgentRequest struct {
	Workspace    string
	Prompt       string
	MCP          *AgentMCP
	Environment  map[string]string
	Budget       TaskBudget
	OutputSchema []byte
}

// AgentResult is the normalized evidence produced by one external-agent run.
// ToolCalls counts agent tool invocations observed in the agent's structured
// event stream. ToolTrace retains compact MCP metadata plus bounded redacted
// Metis arguments. Raw event transcripts remain in-process by default;
// terminal budget overruns persist them as diagnostic sidecars so every
// observed call can be audited.
type ToolCallEvidence struct {
	Name          string `json:"name"`
	DurationMS    int64  `json:"duration_ms"`
	RequestBytes  int    `json:"request_bytes"`
	ResponseBytes int    `json:"response_bytes"`
	Arguments     string `json:"arguments,omitempty"`
	Response      string `json:"response,omitempty"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}

type AgentResult struct {
	Output        string
	ToolCalls     int
	ContextTokens int
	OutputTokens  int
	Transcript    []byte
	ToolTrace     []ToolCallEvidence
}

// AgentDriver opens one conversation for one question. A repair may add a
// second turn, but the next question always gets a clean process and context.
type AgentDriver interface {
	Identity(ctx context.Context) (AgentIdentity, error)
	Open(ctx context.Context, request AgentRequest) (AgentSession, error)
}

// AgentSession is a real Agent-owned model/tool loop. Run is one user turn and
// Close terminates the process after the question (and optional repair).
type AgentSession interface {
	Run(ctx context.Context, request AgentRequest) (AgentResult, error)
	Close() error
}
