package s2sbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

type deterministicRepairRunner struct {
	path        Path
	repairCalls int
}

func (r *deterministicRepairRunner) Path() Path { return r.path }

func (r *deterministicRepairRunner) Answer(_ context.Context, _ string, scenario scenarios.Scenario, _ TaskBudget) (Attempt, error) {
	return Attempt{
		Index:         1,
		SQL:           "broken:" + scenario.Name,
		ToolCalls:     1,
		ContextTokens: 10,
		OutputTokens:  2,
	}, nil
}

func (r *deterministicRepairRunner) Repair(_ context.Context, _ string, scenario scenarios.Scenario, _ TaskBudget, failure error) (Attempt, error) {
	r.repairCalls++
	if failure == nil || !strings.Contains(failure.Error(), "synthetic SQL failure") {
		return Attempt{Index: 2}, fmt.Errorf("repair did not receive the execution error")
	}
	return Attempt{
		Index:         2,
		SQL:           "scenario:" + scenario.Name,
		ToolCalls:     2,
		ContextTokens: 20,
		OutputTokens:  4,
	}, nil
}

type failFirstExecution struct {
	deterministicExecution
	failed int
}

func (e *failFirstExecution) RunSQL(ctx context.Context, sql string, params ...sql.QueryParameter) (scenarios.ResultSet, error) {
	if strings.HasPrefix(sql, "broken:") {
		e.failed++
		return scenarios.ResultSet{}, fmt.Errorf("synthetic SQL failure")
	}
	return e.deterministicExecution.RunSQL(ctx, sql, params...)
}

func TestRunWithExecutionOwnsRepairAndPreservesBothTries(t *testing.T) {
	budget := FrozenBudget
	budget.Repetitions = 1
	runner := &deterministicRepairRunner{path: PathRawAssets}
	execution := &failFirstExecution{}

	attempts, err := RunWithExecution(context.Background(), runner, execution, budget)
	if err != nil {
		t.Fatalf("RunWithExecution: %v", err)
	}
	if len(attempts) != 25 {
		t.Fatalf("attempt count = %d, want 25", len(attempts))
	}
	if runner.repairCalls != 25 || execution.failed != 25 {
		t.Fatalf("repairs=%d first failures=%d, want 25 each", runner.repairCalls, execution.failed)
	}
	for _, attempt := range attempts {
		if attempt.Verdict != VerdictCorrect || attempt.Index != 2 {
			t.Fatalf("%s verdict=%q index=%d, want correct on repair", attempt.Scenario, attempt.Verdict, attempt.Index)
		}
		if len(attempt.Trace) != 2 {
			t.Fatalf("%s trace has %d entries, want 2", attempt.Scenario, len(attempt.Trace))
		}
		if !strings.HasPrefix(attempt.Trace[0].SQL, "broken:") || !strings.Contains(attempt.Trace[0].Error, "synthetic SQL failure") {
			t.Fatalf("%s first trace entry lost failure evidence: %+v", attempt.Scenario, attempt.Trace[0])
		}
		if attempt.Trace[1].SQL != attempt.SQL || attempt.Trace[1].Error != "" {
			t.Fatalf("%s final trace entry does not describe repaired answer: %+v", attempt.Scenario, attempt.Trace[1])
		}
	}

	report, err := Summarise(PathRawAssets, attempts)
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}
	for stratum, summary := range report.ByStratum {
		if summary.FirstTryRight != 0 {
			t.Fatalf("%s first-try correct = %d, want 0 after every answer required repair", stratum, summary.FirstTryRight)
		}
	}
}

type sharedToolBudgetRepairRunner struct {
	repairBudget int
	overrun      bool
}

func (r *sharedToolBudgetRepairRunner) Path() Path { return PathRawAssets }

func (r *sharedToolBudgetRepairRunner) Answer(_ context.Context, _ string, scenario scenarios.Scenario, _ TaskBudget) (Attempt, error) {
	return Attempt{Index: 1, SQL: "broken:" + scenario.Name, ToolCalls: 13}, nil
}

func (r *sharedToolBudgetRepairRunner) Repair(_ context.Context, _ string, scenario scenarios.Scenario, budget TaskBudget, _ error) (Attempt, error) {
	r.repairBudget = budget.ToolCalls
	used := budget.ToolCalls
	if r.overrun {
		used++
	}
	attempt := Attempt{Index: 2, SQL: "scenario:" + scenario.Name, ToolCalls: used}
	if r.overrun {
		return attempt, NewToolBudgetExceededError(used, budget.ToolCalls)
	}
	return attempt, nil
}

func TestRepairReceivesRemainingSharedToolBudget(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	budget := FrozenBudget
	budget.Repetitions = 1
	runner := &sharedToolBudgetRepairRunner{}

	attempts, err := RunWithExecutionObservedScenarios(context.Background(), runner, &failFirstExecution{}, budget, selected[:1], nil)
	if err != nil {
		t.Fatalf("RunWithExecutionObservedScenarios: %v", err)
	}
	wantRemaining := budget.ToolCalls - 13
	if runner.repairBudget != wantRemaining {
		t.Fatalf("repair tool budget = %d, want %d", runner.repairBudget, wantRemaining)
	}
	if len(attempts) != 1 || attempts[0].Verdict != VerdictCorrect || len(attempts[0].Trace) != 2 {
		t.Fatalf("repaired attempt = %+v", attempts)
	}
	if got := attempts[0].Trace[0].ToolCalls + attempts[0].Trace[1].ToolCalls; got != budget.ToolCalls {
		t.Fatalf("cumulative tool calls = %d, want %d", got, budget.ToolCalls)
	}
}

func TestRepairOverrunIsReportedAgainstSharedToolBudget(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	budget := FrozenBudget
	budget.Repetitions = 1
	runner := &sharedToolBudgetRepairRunner{overrun: true}

	attempts, err := RunWithExecutionObservedScenarios(context.Background(), runner, &failFirstExecution{}, budget, selected[:1], nil)
	if err != nil {
		t.Fatalf("RunWithExecutionObservedScenarios: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Verdict != VerdictFailed || len(attempts[0].Trace) != 2 {
		t.Fatalf("overrun attempt = %+v", attempts)
	}
	var exceeded *ToolBudgetExceededError
	if !errors.As(attempts[0].Err, &exceeded) {
		t.Fatalf("error = %v, want ToolBudgetExceededError", attempts[0].Err)
	}
	if exceeded.Used != budget.ToolCalls+1 || exceeded.Budget != budget.ToolCalls {
		t.Fatalf("shared overrun = %+v, want used=%d budget=%d", exceeded, budget.ToolCalls+1, budget.ToolCalls)
	}
	if got := attempts[0].Trace[0].ToolCalls + attempts[0].Trace[1].ToolCalls; got != budget.ToolCalls+1 {
		t.Fatalf("cumulative trace calls = %d, want %d", got, budget.ToolCalls+1)
	}
}

func TestRunWithExecutionSkipsRepairWhenTotalAttemptsIsOne(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	budget := FrozenBudget
	budget.Attempts = 1
	budget.Repetitions = 1
	runner := &deterministicRepairRunner{path: PathRawAssets}

	attempts, err := RunWithExecutionObservedScenarios(context.Background(), runner, &failFirstExecution{}, budget, selected[:1], nil)
	if err != nil {
		t.Fatalf("RunWithExecutionObservedScenarios: %v", err)
	}
	if runner.repairCalls != 0 {
		t.Fatalf("repair calls=%d, want 0", runner.repairCalls)
	}
	if len(attempts) != 1 || attempts[0].Index != 1 || attempts[0].Verdict != VerdictFailed || len(attempts[0].Trace) != 1 {
		t.Fatalf("single-attempt evidence=%+v, want one failed first attempt", attempts)
	}
}

type timeoutRepairRunner struct {
	repairCalls int
}

func (r *timeoutRepairRunner) Path() Path { return PathRawAssets }

func (r *timeoutRepairRunner) Answer(context.Context, string, scenarios.Scenario, TaskBudget) (Attempt, error) {
	return Attempt{Index: 1, ToolCalls: 3, ContextTokens: 100, OutputTokens: 10}, NewAgentTurnTimeoutError("deadline")
}

func (r *timeoutRepairRunner) Repair(context.Context, string, scenarios.Scenario, TaskBudget, error) (Attempt, error) {
	r.repairCalls++
	return Attempt{Index: 2}, nil
}

func TestRunWithExecutionDoesNotRepairTerminalAgentTimeout(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	budget := FrozenBudget
	budget.Repetitions = 1
	runner := &timeoutRepairRunner{}
	attempts, err := RunWithExecutionObservedScenarios(context.Background(), runner, &deterministicExecution{}, budget, selected[:1], nil)
	if err != nil {
		t.Fatalf("RunWithExecutionObservedScenarios: %v", err)
	}
	if runner.repairCalls != 0 {
		t.Fatalf("repair calls = %d, want 0", runner.repairCalls)
	}
	if len(attempts) != 1 || attempts[0].Index != 1 || attempts[0].Verdict != VerdictFailed || !IsAgentTurnTimeoutError(attempts[0].Err) {
		t.Fatalf("timeout attempt = %+v", attempts)
	}
}

type deadlineRepairRunner struct {
	repairCalls int
}

func (r *deadlineRepairRunner) Path() Path { return PathRawAssets }

func (r *deadlineRepairRunner) Answer(ctx context.Context, _ string, _ scenarios.Scenario, _ TaskBudget) (Attempt, error) {
	<-ctx.Done()
	return Attempt{Index: 1, ToolCalls: 1}, ctx.Err()
}

func (r *deadlineRepairRunner) Repair(context.Context, string, scenarios.Scenario, TaskBudget, error) (Attempt, error) {
	r.repairCalls++
	return Attempt{Index: 2}, nil
}

func (r *deadlineRepairRunner) CloseSession() error {
	return fmt.Errorf("synthetic close failure after deadline")
}

func TestQuestionDeadlineIsTerminalFailedAndSkipsRepair(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	budget := FrozenBudget
	budget.Repetitions = 1
	budget.QuestionTimeoutMS = 20
	runner := &deadlineRepairRunner{}
	started := time.Now()
	attempts, err := RunWithExecutionObservedScenarios(context.Background(), runner, &deterministicExecution{}, budget, selected[:1], nil)
	if err != nil {
		t.Fatalf("RunWithExecutionObservedScenarios: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("question deadline took %v, want bounded completion", elapsed)
	}
	if runner.repairCalls != 0 {
		t.Fatalf("repair calls = %d, want 0", runner.repairCalls)
	}
	if len(attempts) != 1 || attempts[0].Verdict != VerdictFailed || !IsAgentTurnTimeoutError(attempts[0].Err) {
		t.Fatalf("deadline attempt = %+v", attempts)
	}
	if attempts[0].DurationMS < int64(budget.QuestionTimeoutMS) {
		t.Fatalf("deadline duration = %dms, want at least %dms", attempts[0].DurationMS, budget.QuestionTimeoutMS)
	}
}
