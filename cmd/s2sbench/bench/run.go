package s2sbench

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

// AttemptEvidence is the immutable evidence for one try at a question. A
// successful second try must not erase the first query or the error that caused
// the repair; first-try success is a headline metric and the raw record must be
// sufficient to audit it later.
type AttemptEvidence struct {
	Index int `json:"attempt_index"`
	// Prompt and Transcript remain available for in-process protocol checks but
	// are deliberately excluded from compact persisted benchmark evidence.
	Prompt        string               `json:"-"`
	StartedAt     time.Time            `json:"started_at,omitempty"`
	DurationMS    int64                `json:"duration_ms"`
	SQL           string               `json:"sql,omitempty"`
	Parameters    []sql.QueryParameter `json:"parameters,omitempty"`
	SemanticQuery string               `json:"semantic_query,omitempty"`
	ToolCalls     int                  `json:"tool_calls"`
	ContextTokens int                  `json:"context_tokens"`
	OutputTokens  int                  `json:"output_tokens"`
	Transcript    string               `json:"-"`
	ToolTrace     []ToolCallEvidence   `json:"tool_trace,omitempty"`
	Result        scenarios.ResultSet  `json:"result"`
	Error         string               `json:"error,omitempty"`
}

// Attempt is what one arm produced for one question on one repetition.
//
// SQL and SemanticQuery are recorded rather than judged. The oracle scores the
// result, but a benchmark that keeps only verdicts cannot answer the question
// anyone will actually ask afterwards -- what did it get wrong, and how? The
// wrong answers are the finding; the counts are the summary.
type Attempt struct {
	Path       Path
	Scenario   string
	Repetition int
	Index      int // 1 for first-try success, 2 when the frozen repair was used
	Question   string
	Prompt     string
	StartedAt  time.Time
	DurationMS int64

	SQL           string               // final executable SQL; direct on raw_assets, compiled on metis
	Parameters    []sql.QueryParameter `json:"parameters,omitempty"`
	SemanticQuery string               // path metis, as sent
	ToolCalls     int
	ContextTokens int    // model input/context tokens consumed by the final try
	OutputTokens  int    // model output tokens produced by the final try
	Transcript    string // exact structured external-agent event stream for the final try
	ToolTrace     []ToolCallEvidence
	Trace         []AttemptEvidence

	Result  scenarios.ResultSet
	Verdict Verdict
	Reason  string // why the verdict, when it is not correct
	Err     error  // transport, execution, or compilation failure
}

// Runner is one benchmark arm. Live v0 uses AgentRunner, which delegates the
// complete model/tool loop to an installed local Agent CLI. Deterministic tests
// may provide in-process runners. The harness owns only the frozen question,
// attempt budget, execution seam, oracle, and evidence collection.
//
// Both arms receive the identical question and budget. The only intentional
// difference is the semantic interface: raw semantic assets for PathRawAssets,
// Metis MCP for PathMetis.
type Runner interface {
	Path() Path
	// Answer returns the result the arm produced, and how it got there. An
	// arm that cannot answer returns a zero ResultSet and a non-nil error;
	// that is a VerdictFailed, not a VerdictWrong.
	Answer(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget) (Attempt, error)
}

// Execution is the benchmark fixture interface. SQL and positional parameters
// are passed separately to the underlying database driver.
type Execution interface {
	Prepare(ctx context.Context, scenario scenarios.Scenario) error
	RunSQL(ctx context.Context, sql string, params ...sql.QueryParameter) (scenarios.ResultSet, error)
}

// scenarioPreparer is implemented by execution-backed runner wrappers. Setup is
// deliberately outside Answer: a broken fixture is benchmark infrastructure
// failure and must abort collection rather than being scored against the model.
type scenarioPreparer interface {
	PrepareScenario(ctx context.Context, scenario scenarios.Scenario) error
}

type sessionCloser interface {
	CloseSession() error
}

type sessionStarter interface {
	BeginSession(repetition int) error
}

// AttemptObserver receives a complete scored repetition immediately after it
// finishes. Live collection uses it for an fsynced JSONL journal; deterministic
// callers may omit it.
type AttemptObserver func(AttemptRecord) error

// Run executes every question in a fresh Agent conversation and judges it
// independently. A repair turn for that question may reuse the conversation,
// but no context crosses a question boundary.
//
// Repetitions are not averaged here. A model's spread across identical runs is
// one of the four things being measured, and averaging it away at collection
// time destroys it irrecoverably.
func Run(ctx context.Context, runner Runner, budget TaskBudget) ([]Attempt, error) {
	return RunObserved(ctx, runner, budget, nil)
}

func RunObserved(ctx context.Context, runner Runner, budget TaskBudget, observer AttemptObserver) ([]Attempt, error) {
	selected, err := SelectedScenarios()
	if err != nil {
		return nil, err
	}
	return RunObservedScenarios(ctx, runner, budget, selected, observer)
}

// RunObservedScenarios executes an explicit ordered subset selected by a
// validated manifest. Formal and diagnostic callers pass all frozen scenarios;
// smoke callers pass exactly one.
func RunObservedScenarios(ctx context.Context, runner Runner, budget TaskBudget, selected []scenarios.Scenario, observer AttemptObserver) ([]Attempt, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one benchmark scenario is required")
	}
	var attempts []Attempt
	for repetition := 1; repetition <= budget.Repetitions; repetition++ {
		for _, scenario := range selected {
			attempt, recorded, questionErr := runObservedQuestion(ctx, runner, budget, scenario, repetition, observer)
			if recorded {
				attempts = append(attempts, attempt)
			}
			if questionErr != nil {
				return attempts, questionErr
			}
		}
	}
	return attempts, nil
}

// runObservedQuestion owns the complete lifecycle for one independently
// scored question. Session setup happens before fixture preparation so every
// exit path can close a partially opened external Agent process.
func runObservedQuestion(ctx context.Context, runner Runner, budget TaskBudget, scenario scenarios.Scenario, repetition int, observer AttemptObserver) (Attempt, bool, error) {
	if starter, ok := runner.(sessionStarter); ok {
		if err := starter.BeginSession(repetition); err != nil {
			return Attempt{}, false, fmt.Errorf("begin %s agent session for %q repetition %d: %w", runner.Path(), scenario.Name, repetition, err)
		}
	}

	var attempt Attempt
	recorded := false
	cancelQuestion := func() {}
	questionErr := func() error {
		if preparer, ok := runner.(scenarioPreparer); ok {
			if err := preparer.PrepareScenario(ctx, scenario); err != nil {
				return fmt.Errorf("prepare benchmark scenario %q: %w", scenario.Name, err)
			}
		}
		question, err := QuestionFor(scenario.Name)
		if err != nil {
			return err
		}
		startedAt := time.Now().UTC()
		questionCtx := ctx
		if budget.QuestionTimeoutMS > 0 {
			questionCtx, cancelQuestion = context.WithTimeout(ctx, time.Duration(budget.QuestionTimeoutMS)*time.Millisecond)
		}
		var answerErr error
		attempt, answerErr = runner.Answer(questionCtx, question, scenario, budget)
		questionDuration := time.Since(startedAt)
		timedOut := questionCtx.Err() == context.DeadlineExceeded ||
			(budget.QuestionTimeoutMS > 0 && questionDuration >= time.Duration(budget.QuestionTimeoutMS)*time.Millisecond)
		if timedOut && !IsToolBudgetExceededError(answerErr) && !IsAgentTurnTimeoutError(answerErr) {
			answerErr = NewAgentTurnTimeoutError(fmt.Sprintf("question exceeded %dms wall-clock budget", budget.QuestionTimeoutMS))
		}
		attempt.Path = runner.Path()
		attempt.Scenario = scenario.Name
		attempt.Repetition = repetition
		attempt.Question = question
		attempt.StartedAt = startedAt
		attempt.DurationMS = questionDuration.Milliseconds()

		// The arm reports how many attempts it took, and nothing else can
		// know. An arm that leaves it zero would have every correct answer
		// counted as a first-try success -- first-try rate is one of the
		// four headline numbers, and it would be inflated silently, in the
		// direction that flatters whichever arm forgot.
		if attempt.Index < 1 {
			return fmt.Errorf("%s did not report an attempt index for %q; first-try success cannot be measured",
				runner.Path(), scenario.Name)
		}
		if attempt.Index > budget.Attempts {
			return fmt.Errorf("%s used %d attempts on %q, budget is %d",
				runner.Path(), attempt.Index, scenario.Name, budget.Attempts)
		}
		if attempt.ToolCalls < 0 {
			return fmt.Errorf("%s used %d tool calls on %q, budget is 0..%d",
				runner.Path(), attempt.ToolCalls, scenario.Name, budget.ToolCalls)
		}
		if attempt.ToolCalls > budget.ToolCalls && !IsToolBudgetExceededError(answerErr) {
			return fmt.Errorf("%s used %d tool calls on %q without a matching terminal budget error; budget is 0..%d",
				runner.Path(), attempt.ToolCalls, scenario.Name, budget.ToolCalls)
		}
		if attempt.ContextTokens < 0 || attempt.OutputTokens < 0 {
			return fmt.Errorf("%s reported negative token usage on %q: context=%d output=%d",
				runner.Path(), scenario.Name, attempt.ContextTokens, attempt.OutputTokens)
		}

		switch {
		case answerErr != nil:
			attempt.Verdict = VerdictFailed
			attempt.Err = answerErr
			attempt.Reason = answerErr.Error()
		default:
			verdict, reason := Judge(scenario, attempt.Result)
			attempt.Verdict = verdict
			if reason != nil {
				attempt.Reason = reason.Error()
			}
		}
		// Provider/process availability is run infrastructure, not a semantic
		// answer. Abort immediately but leave this identity incomplete so a
		// resume can retry it without turning an outage into a scored failure.
		if IsAgentUnavailableError(answerErr) {
			return fmt.Errorf("%s cannot continue after %q: %w", runner.Path(), scenario.Name, answerErr)
		}
		recorded = true
		if observer != nil {
			record, err := NewAttemptRecord(question, scenario, attempt)
			if err != nil {
				return fmt.Errorf("build incremental evidence for %s/%s repetition %d: %w", runner.Path(), scenario.Name, repetition, err)
			}
			if err := observer(record); err != nil {
				return fmt.Errorf("persist incremental evidence for %s/%s repetition %d: %w", runner.Path(), scenario.Name, repetition, err)
			}
		}
		return nil
	}()
	if closer, ok := runner.(sessionCloser); ok {
		if err := closer.CloseSession(); err != nil && questionErr == nil && !IsAgentTurnTimeoutError(attempt.Err) {
			questionErr = fmt.Errorf("close %s agent session for %q repetition %d: %w", runner.Path(), scenario.Name, repetition, err)
		}
	}
	cancelQuestion()
	return attempt, recorded, questionErr
}

// Report is what one arm's run produced, summarised the way the issue asks:
// semantic accuracy, cross-run consistency, context tokens, first-try success.
type Report struct {
	Path         Path
	ByStratum    map[Stratum]StratumReport
	Inconsistent []string // scenarios whose verdict was not the same across every repetition
}

// StratumReport keeps wrong and failed apart. A model that refuses and a model
// that confidently misreports are different products, and the difference is the
// thesis: collapsing them into one accuracy number would erase the finding the
// benchmark exists to produce.
type StratumReport struct {
	Questions     int
	Correct       int
	Wrong         int
	Failed        int
	FirstTryRight int
}

// Summarise turns attempts into a report. It does not decide whether a
// difference is real -- with five repetitions there is not enough data for
// that, and a benchmark that reports significance it cannot support is worse
// than one that reports counts.
func Summarise(path Path, attempts []Attempt) (Report, error) {
	selection, err := Selection()
	if err != nil {
		return Report{}, err
	}
	stratumOf := map[string]Stratum{}
	for _, stratum := range Strata {
		for _, scenario := range selection[stratum] {
			stratumOf[scenario.Name] = stratum
		}
	}

	report := Report{Path: path, ByStratum: map[Stratum]StratumReport{}}
	verdicts := map[string]map[Verdict]int{}
	counted := map[string]struct{}{}

	for _, attempt := range attempts {
		stratum, ok := stratumOf[attempt.Scenario]
		if !ok {
			return Report{}, fmt.Errorf("attempt names scenario %q, which is not in the selection", attempt.Scenario)
		}
		entry := report.ByStratum[stratum]
		if _, seen := counted[attempt.Scenario]; !seen {
			counted[attempt.Scenario] = struct{}{}
			entry.Questions++
		}
		switch attempt.Verdict {
		case VerdictCorrect:
			entry.Correct++
			if attempt.Index <= 1 {
				entry.FirstTryRight++
			}
		case VerdictWrong:
			entry.Wrong++
		case VerdictFailed:
			entry.Failed++
		default:
			return Report{}, fmt.Errorf("attempt on %q has no verdict", attempt.Scenario)
		}
		report.ByStratum[stratum] = entry

		if verdicts[attempt.Scenario] == nil {
			verdicts[attempt.Scenario] = map[Verdict]int{}
		}
		verdicts[attempt.Scenario][attempt.Verdict]++
	}

	for scenario, seen := range verdicts {
		if len(seen) > 1 {
			report.Inconsistent = append(report.Inconsistent, scenario)
		}
	}
	sort.Strings(report.Inconsistent)
	return report, nil
}
