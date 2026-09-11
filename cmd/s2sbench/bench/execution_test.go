package s2sbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestToolBudgetOverrunTakesPrecedenceOverQuestionDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantUsed := FrozenBudget.ToolCalls + 1
	got := normalizeQuestionTimeout(ctx, NewToolBudgetExceededError(wantUsed, FrozenBudget.ToolCalls), FrozenBudget)
	var exceeded *ToolBudgetExceededError
	if !errors.As(got, &exceeded) || exceeded.Used != wantUsed || exceeded.Budget != FrozenBudget.ToolCalls {
		t.Fatalf("normalized error = %v, want tool budget %d/%d", got, wantUsed, FrozenBudget.ToolCalls)
	}
}

type deterministicSQLRunner struct {
	path Path
}

func (r deterministicSQLRunner) Path() Path { return r.path }

func (r deterministicSQLRunner) Answer(_ context.Context, _ string, scenario scenarios.Scenario, _ TaskBudget) (Attempt, error) {
	attempt := Attempt{
		Index:         1,
		ToolCalls:     1,
		ContextTokens: 10,
		OutputTokens:  5,
		SQL:           "scenario:" + scenario.Name,
		// Deliberately wrong. RunWithExecution must overwrite this instead of
		// letting a runner bypass execution by handing the oracle answer rows.
		Result: scenarios.ResultSet{},
	}
	if r.path == PathMetis {
		attempt.SemanticQuery = `{"scenario":"` + scenario.Name + `"}`
	}
	return attempt, nil
}

type overBudgetRunner struct{}

func (overBudgetRunner) Path() Path { return PathRawAssets }

func (overBudgetRunner) Answer(_ context.Context, _ string, _ scenarios.Scenario, budget TaskBudget) (Attempt, error) {
	used := budget.ToolCalls + 1
	return Attempt{Index: 1, ToolCalls: used, ContextTokens: 42, OutputTokens: 3}, NewToolBudgetExceededError(used, budget.ToolCalls)
}

type deterministicExecution struct {
	prepared      map[fixtures.ID]int
	activeFixture fixtures.ID
	runs          int
}

func (e *deterministicExecution) Prepare(_ context.Context, scenario scenarios.Scenario) error {
	if e.prepared == nil {
		e.prepared = map[fixtures.ID]int{}
	}
	if _, ok := fixtures.Lookup(scenario.Fixture); !ok {
		return fmt.Errorf("unknown fixture %q", scenario.Fixture)
	}
	e.prepared[scenario.Fixture]++
	e.activeFixture = scenario.Fixture
	return nil
}

func (e *deterministicExecution) RunSQL(_ context.Context, sql string, params ...sql.QueryParameter) (scenarios.ResultSet, error) {
	const prefix = "scenario:"
	if !strings.HasPrefix(sql, prefix) {
		return scenarios.ResultSet{}, fmt.Errorf("unexpected SQL %q", sql)
	}
	name := strings.TrimPrefix(sql, prefix)
	for _, scenario := range scenarios.Core {
		if scenario.Name != name {
			continue
		}
		if e.activeFixture != scenario.Fixture {
			return scenarios.ResultSet{}, fmt.Errorf("scenario %q executed with active fixture %q, want %q", name, e.activeFixture, scenario.Fixture)
		}
		if scenario.ExpectedResult == nil {
			return scenarios.ResultSet{}, fmt.Errorf("scenario %q has no expected result", name)
		}
		e.runs++
		return scenario.ExpectedResult.ResultSet, nil
	}
	return scenarios.ResultSet{}, fmt.Errorf("unknown scenario %q", name)
}

func TestRunWithExecutionExercisesBothArmsThroughCanonicalFixtures(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatalf("SelectedScenarios: %v", err)
	}
	wantPrepareCalls := map[fixtures.ID]int{}
	for _, scenario := range selected {
		wantPrepareCalls[scenario.Fixture]++
	}

	budget := FrozenBudget
	budget.Repetitions = 1 // deterministic harness check, not a collectable v0 run
	for _, path := range Paths {
		t.Run(string(path), func(t *testing.T) {
			execution := &deterministicExecution{}
			attempts, err := RunWithExecution(context.Background(), deterministicSQLRunner{path: path}, execution, budget)
			if err != nil {
				t.Fatalf("RunWithExecution: %v", err)
			}
			if len(attempts) != len(selected) {
				t.Fatalf("attempt count = %d, want %d", len(attempts), len(selected))
			}
			for _, attempt := range attempts {
				if attempt.Verdict != VerdictCorrect {
					t.Fatalf("%s verdict = %q: %s", attempt.Scenario, attempt.Verdict, attempt.Reason)
				}
				if strings.TrimSpace(attempt.SQL) == "" {
					t.Fatalf("%s did not retain executable SQL evidence", attempt.Scenario)
				}
				if path == PathMetis && strings.TrimSpace(attempt.SemanticQuery) == "" {
					t.Fatalf("%s did not retain semantic-query evidence", attempt.Scenario)
				}
			}
			if execution.runs != len(selected) {
				t.Fatalf("execution runs = %d, want %d", execution.runs, len(selected))
			}
			if len(execution.prepared) != len(wantPrepareCalls) {
				t.Fatalf("prepared fixtures = %d, want %d", len(execution.prepared), len(wantPrepareCalls))
			}
			for fixture, want := range wantPrepareCalls {
				if got := execution.prepared[fixture]; got != want {
					t.Fatalf("fixture %q prepared %d times, want %d (once per selected scenario)", fixture, got, want)
				}
			}
		})
	}
}

func TestRunWithExecutionJournalsPerQuestionToolBudgetFailure(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	budget := FrozenBudget
	budget.Repetitions = 1
	var records []AttemptRecord
	attempts, err := RunWithExecutionObservedScenarios(context.Background(), overBudgetRunner{}, &deterministicExecution{}, budget, selected[:1], func(record AttemptRecord) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		t.Fatalf("run should continue after recording per-question budget failure: %v", err)
	}
	if len(attempts) != 1 || len(records) != 1 {
		t.Fatalf("attempts/records=%d/%d, want 1/1", len(attempts), len(records))
	}
	record := records[0]
	if record.Verdict != VerdictFailed || record.ToolCalls != budget.ToolCalls+1 || record.ContextTokens != 42 || len(record.Trace) != 1 {
		t.Fatalf("recorded budget failure=%+v", record)
	}
	if !strings.Contains(record.Error, "exceeded tool-call budget") || !strings.Contains(record.Trace[0].Error, "exceeded tool-call budget") {
		t.Fatalf("budget error was not retained in record and trace: %+v", record)
	}
}

func TestRunWithExecutionRequiresTheSharedExecutionSeam(t *testing.T) {
	budget := FrozenBudget
	budget.Repetitions = 1
	_, err := RunWithExecution(context.Background(), deterministicSQLRunner{path: PathRawAssets}, nil, budget)
	if err == nil || !strings.Contains(err.Error(), "execution is required") {
		t.Fatalf("error = %v, want missing-execution refusal", err)
	}
}

type noSQLRunner struct{ path Path }

func (r noSQLRunner) Path() Path { return r.path }
func (r noSQLRunner) Answer(context.Context, string, scenarios.Scenario, TaskBudget) (Attempt, error) {
	return Attempt{Index: 1}, nil
}

type unavailableRepairRunner struct {
	answers int
	repairs int
}

func (r *unavailableRepairRunner) Path() Path { return PathRawAssets }
func (r *unavailableRepairRunner) Answer(context.Context, string, scenarios.Scenario, TaskBudget) (Attempt, error) {
	r.answers++
	return Attempt{Index: 1, Prompt: "probe"}, NewAgentUnavailableError(fmt.Errorf("requested model is unsupported"))
}
func (r *unavailableRepairRunner) Repair(context.Context, string, scenarios.Scenario, TaskBudget, error) (Attempt, error) {
	r.repairs++
	return Attempt{Index: 2}, nil
}

func TestRunWithExecutionAbortsWithoutScoringUnavailableAgentError(t *testing.T) {
	runner := &unavailableRepairRunner{}
	budget := FrozenBudget
	budget.Repetitions = 1
	var records []AttemptRecord
	attempts, err := RunWithExecutionObserved(context.Background(), runner, &deterministicExecution{}, budget, func(record AttemptRecord) error {
		records = append(records, record)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "cannot continue") {
		t.Fatalf("error = %v, want fail-fast error", err)
	}
	if runner.answers != 1 || runner.repairs != 0 {
		t.Fatalf("agent calls = answers:%d repairs:%d, want 1/0", runner.answers, runner.repairs)
	}
	if len(attempts) != 0 || len(records) != 0 {
		t.Fatalf("attempts/records = %d/%d, want 0/0 for infrastructure outage", len(attempts), len(records))
	}
}

func TestRunWithExecutionClassifiesMissingSQLAsFailed(t *testing.T) {
	budget := FrozenBudget
	budget.Repetitions = 1
	attempts, err := RunWithExecution(context.Background(), noSQLRunner{path: PathRawAssets}, &deterministicExecution{}, budget)
	if err != nil {
		t.Fatalf("RunWithExecution: %v", err)
	}
	if len(attempts) == 0 {
		t.Fatal("RunWithExecution returned no attempts")
	}
	for _, attempt := range attempts {
		if attempt.Verdict != VerdictFailed {
			t.Fatalf("%s verdict = %q, want failed", attempt.Scenario, attempt.Verdict)
		}
		if !strings.Contains(attempt.Reason, "no executable SQL") {
			t.Fatalf("%s reason = %q, want missing-SQL evidence", attempt.Scenario, attempt.Reason)
		}
	}
}
