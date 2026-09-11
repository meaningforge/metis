package s2sbench

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

// RunWithExecution is the production benchmark path: the runner may decide how
// to obtain SQL, but it cannot supply the rows that are scored. Every successful
// answer is executed against the same canonical conformance fixture seam and
// only those normalized rows reach the oracle.
func RunWithExecution(ctx context.Context, runner Runner, execution Execution, budget TaskBudget) ([]Attempt, error) {
	return RunWithExecutionObserved(ctx, runner, execution, budget, nil)
}

func RunWithExecutionObserved(ctx context.Context, runner Runner, execution Execution, budget TaskBudget, observer AttemptObserver) ([]Attempt, error) {
	selected, err := SelectedScenarios()
	if err != nil {
		return nil, err
	}
	return RunWithExecutionObservedScenarios(ctx, runner, execution, budget, selected, observer)
}

func RunWithExecutionObservedScenarios(ctx context.Context, runner Runner, execution Execution, budget TaskBudget, selected []scenarios.Scenario, observer AttemptObserver) ([]Attempt, error) {
	if execution == nil {
		return nil, fmt.Errorf("execution is required")
	}
	return RunObservedScenarios(ctx, &executingRunner{runner: runner, execution: execution}, budget, selected, observer)
}

type executingRunner struct {
	runner    Runner
	execution Execution
}

func (r *executingRunner) BeginSession(repetition int) error {
	if starter, ok := r.runner.(sessionStarter); ok {
		return starter.BeginSession(repetition)
	}
	return nil
}

func (r *executingRunner) CloseSession() error {
	if closer, ok := r.runner.(sessionCloser); ok {
		return closer.CloseSession()
	}
	return nil
}

func (r *executingRunner) Path() Path { return r.runner.Path() }

// PrepareScenario is called by Run immediately before the repetitions for one
// scenario. Fixture setup is benchmark infrastructure, not model behavior, so
// any setup failure aborts collection rather than being scored as a failed
// answer. Preparing at the scenario boundary is also required by real engines
// whose canonical fixtures reuse physical table names: preparing all fixtures
// up front would let a later fixture overwrite tables needed by an earlier one.
func (r *executingRunner) PrepareScenario(ctx context.Context, scenario scenarios.Scenario) error {
	return r.execution.Prepare(ctx, scenario)
}

func (r *executingRunner) Answer(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget) (Attempt, error) {
	attempt, failure := r.runner.Answer(ctx, question, scenario, budget)
	failure = normalizeQuestionTimeout(ctx, failure, budget)
	if attempt.Index != 1 {
		return attempt, fmt.Errorf("%s first proposal for %q reports attempt index %d; hidden retries are not auditable", r.runner.Path(), scenario.Name, attempt.Index)
	}
	if err := validateAttemptUsage(attempt, budget, failure); err != nil {
		return attempt, err
	}
	failure = r.execute(ctx, scenario, &attempt, failure)
	trace := []AttemptEvidence{evidenceFor(attempt, 1, failure)}
	usedToolCalls := attempt.ToolCalls
	if failure == nil {
		attempt.Index = 1
		attempt.Trace = trace
		return attempt, nil
	}
	if IsAgentUnavailableError(failure) || IsToolBudgetExceededError(failure) || IsAgentTurnTimeoutError(failure) {
		attempt.Index = 1
		attempt.Trace = trace
		return attempt, failure
	}

	repairer, canRepair := r.runner.(RepairRunner)
	if !canRepair || budget.Attempts < 2 || usedToolCalls >= budget.ToolCalls {
		attempt.Index = 1
		attempt.Trace = trace
		return attempt, failure
	}

	for index := 2; index <= budget.Attempts; index++ {
		repairBudget := budget
		repairBudget.ToolCalls = budget.ToolCalls - usedToolCalls
		if repairBudget.ToolCalls <= 0 {
			break
		}
		repaired, repairErr := repairer.Repair(ctx, question, scenario, repairBudget, failure)
		repairErr = normalizeQuestionTimeout(ctx, repairErr, budget)
		if repaired.Index != index {
			return repaired, fmt.Errorf("%s repair for %q reports attempt index %d, want %d; hidden retries are not auditable", r.runner.Path(), scenario.Name, repaired.Index, index)
		}
		if err := validateAttemptUsage(repaired, repairBudget, repairErr); err != nil {
			return repaired, err
		}
		usedToolCalls += repaired.ToolCalls
		if IsToolBudgetExceededError(repairErr) {
			repairErr = NewToolBudgetExceededError(usedToolCalls, budget.ToolCalls)
		}
		failure = r.execute(ctx, scenario, &repaired, repairErr)
		trace = append(trace, evidenceFor(repaired, index, failure))
		repaired.Index = index
		repaired.Trace = append([]AttemptEvidence(nil), trace...)
		if failure == nil {
			return repaired, nil
		}
		attempt = repaired
	}
	return attempt, failure
}

func normalizeQuestionTimeout(ctx context.Context, failure error, budget TaskBudget) error {
	// A driver may observe the first over-budget call just before the shared
	// question deadline fires. Preserve the earlier, more specific terminal
	// evidence instead of relabelling it as a timeout.
	if IsToolBudgetExceededError(failure) || IsAgentTurnTimeoutError(failure) {
		return failure
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(failure, context.DeadlineExceeded) {
		return NewAgentTurnTimeoutError(fmt.Sprintf("question exceeded %dms wall-clock budget", budget.QuestionTimeoutMS))
	}
	return failure
}

func (r *executingRunner) execute(ctx context.Context, scenario scenarios.Scenario, attempt *Attempt, prior error) error {
	if prior != nil {
		return prior
	}
	sql := strings.TrimSpace(attempt.SQL)
	if sql == "" {
		return fmt.Errorf("%s produced no executable SQL for %q", r.runner.Path(), scenario.Name)
	}
	result, err := r.execution.RunSQL(ctx, sql, attempt.Parameters...)
	if err != nil {
		return fmt.Errorf("execute %s answer for %q: %w", r.runner.Path(), scenario.Name, err)
	}
	// Never trust a runner-supplied ResultSet. The result that gets scored must
	// be exactly what the shared execution seam returned.
	attempt.Result = result
	return nil
}

func validateAttemptUsage(attempt Attempt, budget TaskBudget, failure error) error {
	if attempt.ToolCalls < 0 {
		return fmt.Errorf("attempt %d used %d tool calls, budget is 0..%d", attempt.Index, attempt.ToolCalls, budget.ToolCalls)
	}
	if attempt.ToolCalls > budget.ToolCalls && !IsToolBudgetExceededError(failure) {
		return fmt.Errorf("attempt %d used %d tool calls without a matching terminal budget error; budget is 0..%d", attempt.Index, attempt.ToolCalls, budget.ToolCalls)
	}
	if attempt.ContextTokens < 0 || attempt.OutputTokens < 0 {
		return fmt.Errorf("attempt %d reported negative token usage: context=%d output=%d", attempt.Index, attempt.ContextTokens, attempt.OutputTokens)
	}
	return nil
}

func evidenceFor(attempt Attempt, index int, failure error) AttemptEvidence {
	evidence := AttemptEvidence{
		Index:         index,
		Prompt:        attempt.Prompt,
		StartedAt:     attempt.StartedAt,
		DurationMS:    attempt.DurationMS,
		SQL:           attempt.SQL,
		Parameters:    slices.Clone(attempt.Parameters),
		SemanticQuery: attempt.SemanticQuery,
		ToolCalls:     attempt.ToolCalls,
		ContextTokens: attempt.ContextTokens,
		OutputTokens:  attempt.OutputTokens,
		Transcript:    attempt.Transcript,
		ToolTrace:     append([]ToolCallEvidence(nil), attempt.ToolTrace...),
		Result:        attempt.Result,
	}
	if failure != nil {
		evidence.Error = failure.Error()
	}
	return evidence
}
