package s2sbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

// AgentTaskFactory creates one turn in an isolated external-agent session.
// It may inspect Scenario only to materialize the canonical fixture/workspace;
// expected results must never be written into the request.
type AgentTaskFactory interface {
	BuildAgentRequest(ctx context.Context, path Path, question string, scenario scenarios.Scenario, budget TaskBudget, failure string) (AgentRequest, error)
}

type agentTaskSessionFactory interface {
	BeginSession(repetition int) error
}

// AgentRunner adapts a real local agent to the frozen Runner contract. The
// external agent owns its complete model/tool loop; S2SBench only builds the
// allowed environment, enforces the returned usage budget, preserves Metis
// parameters deterministically, executes final SQL, and scores the result.
type AgentRunner struct {
	Arm     Path
	Driver  AgentDriver
	Tasks   AgentTaskFactory
	session AgentSession
}

func (r *AgentRunner) Path() Path { return r.Arm }

func (r *AgentRunner) BeginSession(repetition int) error {
	if r == nil {
		return fmt.Errorf("agent runner is required")
	}
	if r.session != nil {
		return fmt.Errorf("previous agent session is still open")
	}
	if factory, ok := r.Tasks.(agentTaskSessionFactory); ok {
		return factory.BeginSession(repetition)
	}
	return nil
}

func (r *AgentRunner) Answer(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget) (Attempt, error) {
	return r.run(ctx, question, scenario, budget, 1, "")
}

func (r *AgentRunner) Repair(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget, failure error) (Attempt, error) {
	text := ""
	if failure != nil {
		text = failure.Error()
	}
	return r.run(ctx, question, scenario, budget, 2, text)
}

func (r *AgentRunner) run(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget, index int, failure string) (attempt Attempt, runErr error) {
	startedAt := time.Now().UTC()
	attempt = Attempt{Index: index, Question: question, StartedAt: startedAt}
	defer func() {
		attempt.DurationMS = time.Since(startedAt).Milliseconds()
	}()
	if r == nil || r.Driver == nil {
		return attempt, fmt.Errorf("agent driver is required")
	}
	if r.Tasks == nil {
		return attempt, fmt.Errorf("agent task factory is required")
	}
	if r.Arm != PathRawAssets && r.Arm != PathMetis {
		return attempt, fmt.Errorf("unknown S2SBench path %q", r.Arm)
	}

	request, err := r.Tasks.BuildAgentRequest(ctx, r.Arm, question, scenario, budget, failure)
	if err != nil {
		return attempt, err
	}
	attempt.Prompt = request.Prompt
	if r.session == nil {
		r.session, err = r.Driver.Open(ctx, request)
		if err != nil {
			return attempt, err
		}
	}
	result, err := r.session.Run(ctx, request)
	attempt.DurationMS = time.Since(startedAt).Milliseconds()
	attempt.ToolCalls = result.ToolCalls
	attempt.ContextTokens = result.ContextTokens
	attempt.OutputTokens = result.OutputTokens
	attempt.Transcript = string(result.Transcript)
	attempt.ToolTrace = append([]ToolCallEvidence(nil), result.ToolTrace...)
	if result.ToolCalls < 0 {
		return attempt, fmt.Errorf("external agent reported negative tool calls: %d", result.ToolCalls)
	}
	if result.ContextTokens < 0 || result.OutputTokens < 0 {
		return attempt, fmt.Errorf("external agent reported negative token usage")
	}
	if err != nil {
		return attempt, err
	}
	if result.ToolCalls > budget.ToolCalls {
		return attempt, NewToolBudgetExceededError(result.ToolCalls, budget.ToolCalls)
	}

	physicalQuery, err := normalizeAgentOutput(r.Arm, result.Output)
	attempt.SQL = physicalQuery.SQL
	attempt.Parameters = physicalQuery.Parameters
	if err != nil {
		return attempt, err
	}
	return attempt, nil
}

// CloseSession is called after one question and its optional repair turn.
func (r *AgentRunner) CloseSession() error {
	if r == nil || r.session == nil {
		return nil
	}
	err := r.session.Close()
	r.session = nil
	return err
}

func normalizeAgentOutput(path Path, output string) (sql.SQLRenderResult, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return sql.SQLRenderResult{}, fmt.Errorf("external agent returned no answer")
	}
	if path == PathRawAssets {
		statement, err := unwrapAgentCodeFence(output, "sql")
		return sql.SQLRenderResult{Dialect: "DUCKDB", SQL: statement}, err
	}
	if path != PathMetis {
		return sql.SQLRenderResult{}, fmt.Errorf("unknown S2SBench path %q", path)
	}
	var err error
	output, err = unwrapAgentCodeFence(output, "json")
	if err != nil {
		return sql.SQLRenderResult{}, err
	}

	decoder := json.NewDecoder(bytes.NewBufferString(output))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var physicalQuery sql.SQLRenderResult
	if err := decoder.Decode(&physicalQuery); err != nil {
		return sql.SQLRenderResult{}, fmt.Errorf("decode Metis physical_query returned by external agent: %w; output prefix=%q", err, agentOutputPrefix(output))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return sql.SQLRenderResult{}, fmt.Errorf("external agent returned more than one JSON value for Metis physical_query")
		}
		trailingOutput := strings.TrimSpace(output[decoder.InputOffset():])
		return sql.SQLRenderResult{}, fmt.Errorf("external agent returned one valid Metis physical_query JSON object followed by invalid trailing content; return only the first complete object and remove everything after it: %w; trailing prefix=%q", err, agentOutputPrefix(trailingOutput))
	}
	if physicalQuery.Dialect != "DUCKDB" {
		return sql.SQLRenderResult{}, fmt.Errorf("Metis physical_query dialect is %q, want DUCKDB", physicalQuery.Dialect)
	}
	if strings.TrimSpace(physicalQuery.SQL) == "" {
		return sql.SQLRenderResult{}, fmt.Errorf("Metis physical_query contains empty SQL")
	}
	return physicalQuery, nil
}

func agentOutputPrefix(output string) string {
	const limit = 256
	runes := []rune(output)
	if len(runes) <= limit {
		return output
	}
	return string(runes[:limit]) + "…"
}

// unwrapAgentCodeFence tolerates the common presentation-only wrapper emitted
// by chat models while still rejecting prose, multiple blocks, and a fence for
// the wrong data format. The wrapper has no bearing on the semantic answer.
func unwrapAgentCodeFence(output, format string) (string, error) {
	if !strings.HasPrefix(output, "```") && !strings.HasSuffix(output, "```") {
		return output, nil
	}
	lines := strings.Split(output, "\n")
	if len(lines) < 3 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return "", fmt.Errorf("external agent returned a malformed code fence")
	}
	opener := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "```"))
	if opener != "" && !strings.EqualFold(opener, format) {
		return "", fmt.Errorf("external agent returned %q code fence, want %q", opener, format)
	}
	content := strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
	if content == "" || strings.Contains(content, "```") {
		return "", fmt.Errorf("external agent returned an empty or multiple code fence")
	}
	return content, nil
}
