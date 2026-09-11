package s2sbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

// AttemptRecord is the serializable evidence for one question repetition. The
// top-level fields describe the final try for convenient reporting; Trace keeps
// every try so a successful repair cannot erase the first failure.
type AttemptRecord struct {
	Path       Path   `json:"path"`
	Scenario   string `json:"scenario"`
	Repetition int    `json:"repetition"`
	Index      int    `json:"attempt_index"`
	Question   string `json:"question"`
	// Prompt and Transcript are internal-only. PromptVersion plus Question
	// identifies the frozen input, while raw interaction streams are too large
	// and duplicated to belong in the default result bundle.
	Prompt        string               `json:"-"`
	StartedAt     time.Time            `json:"started_at"`
	DurationMS    int64                `json:"duration_ms"`
	SQL           string               `json:"sql,omitempty"`
	Parameters    []sql.QueryParameter `json:"parameters,omitempty"`
	SemanticQuery string               `json:"semantic_query,omitempty"`
	ToolCalls     int                  `json:"tool_calls"`
	ContextTokens int                  `json:"context_tokens"`
	OutputTokens  int                  `json:"output_tokens"`
	Transcript    string               `json:"-"`
	// PersistTranscript asks the live journal to retain raw per-turn JSONL as
	// diagnostic sidecars. Compact final artifacts still omit transcripts.
	PersistTranscript bool                         `json:"-"`
	ToolTrace         []ToolCallEvidence           `json:"tool_trace,omitempty"`
	Result            scenarios.ResultSet          `json:"result"`
	Expected          *scenarios.ResultExpectation `json:"expected_result,omitempty"`
	Trace             []AttemptEvidence            `json:"trace"`
	Verdict           Verdict                      `json:"verdict"`
	Reason            string                       `json:"reason,omitempty"`
	Error             string                       `json:"error,omitempty"`
}

// ArmRunArtifact is one complete arm of the frozen experiment. Keeping one
// artifact per arm makes collection append-free and crash-safe while the
// manifest keeps both arms tied to the same model and frozen frame.
type ArmRunArtifact struct {
	Manifest RunManifest     `json:"manifest"`
	Path     Path            `json:"path"`
	Attempts []AttemptRecord `json:"attempts"`
}

// NewArmRunArtifact converts the in-memory attempts only after proving they are
// a complete, deterministically ordered run of the manifest's frozen selection
// (all 25 scenarios normally, or one selected scenario in smoke mode).
func NewArmRunArtifact(manifest RunManifest, path Path, attempts []Attempt) (ArmRunArtifact, error) {
	if err := manifest.ValidateForCollection(); err != nil {
		return ArmRunArtifact{}, err
	}
	if path != PathRawAssets && path != PathMetis {
		return ArmRunArtifact{}, fmt.Errorf("unknown benchmark path %q", path)
	}

	selected, err := ScenariosForManifest(manifest)
	if err != nil {
		return ArmRunArtifact{}, err
	}
	wantAttempts := len(selected) * manifest.Budget.Repetitions
	if len(attempts) != wantAttempts {
		return ArmRunArtifact{}, fmt.Errorf("%s run has %d attempts, want complete frozen run of %d", path, len(attempts), wantAttempts)
	}

	type attemptKey struct {
		scenario   string
		repetition int
	}
	type indexedAttempt struct {
		index   int
		attempt Attempt
	}
	allowed := make(map[string]struct{}, len(selected))
	for _, scenario := range selected {
		allowed[scenario.Name] = struct{}{}
	}
	byIdentity := make(map[attemptKey]indexedAttempt, len(attempts))
	for sourceIndex, attempt := range attempts {
		if attempt.Path != path {
			return ArmRunArtifact{}, fmt.Errorf("attempt %d path is %q, want %q", sourceIndex, attempt.Path, path)
		}
		if _, ok := allowed[attempt.Scenario]; !ok {
			return ArmRunArtifact{}, fmt.Errorf("attempt %d scenario %q is not in the manifest", sourceIndex, attempt.Scenario)
		}
		if attempt.Repetition < 1 || attempt.Repetition > manifest.Budget.Repetitions {
			return ArmRunArtifact{}, fmt.Errorf("attempt %d repetition is %d, want 1..%d", sourceIndex, attempt.Repetition, manifest.Budget.Repetitions)
		}
		key := attemptKey{scenario: attempt.Scenario, repetition: attempt.Repetition}
		if previous, exists := byIdentity[key]; exists {
			return ArmRunArtifact{}, fmt.Errorf("attempts %d and %d duplicate %s repetition %d", previous.index, sourceIndex, attempt.Scenario, attempt.Repetition)
		}
		byIdentity[key] = indexedAttempt{index: sourceIndex, attempt: attempt}
	}

	records := make([]AttemptRecord, 0, len(attempts))
	for _, scenario := range selected {
		for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
			located, ok := byIdentity[attemptKey{scenario: scenario.Name, repetition: repetition}]
			if !ok {
				return ArmRunArtifact{}, fmt.Errorf("missing %s attempt for %s repetition %d", path, scenario.Name, repetition)
			}
			attempt := located.attempt
			sourceIndex := located.index
			if attempt.Index < 1 || attempt.Index > manifest.Budget.Attempts {
				return ArmRunArtifact{}, fmt.Errorf("attempt %d reports retry index %d outside budget 1..%d", sourceIndex, attempt.Index, manifest.Budget.Attempts)
			}
			if attempt.ToolCalls < 0 {
				return ArmRunArtifact{}, fmt.Errorf("attempt %d reports %d tool calls outside budget 0..%d", sourceIndex, attempt.ToolCalls, manifest.Budget.ToolCalls)
			}
			if attempt.ToolCalls > manifest.Budget.ToolCalls && !validTerminalToolBudgetOverrun(attempt, manifest.Budget) {
				return ArmRunArtifact{}, fmt.Errorf("attempt %d reports %d tool calls outside budget 0..%d without a typed terminal overrun", sourceIndex, attempt.ToolCalls, manifest.Budget.ToolCalls)
			}
			if attempt.ContextTokens < 0 || attempt.OutputTokens < 0 {
				return ArmRunArtifact{}, fmt.Errorf("attempt %d reports negative token usage: context=%d output=%d", sourceIndex, attempt.ContextTokens, attempt.OutputTokens)
			}
			if attempt.Verdict != VerdictCorrect && attempt.Verdict != VerdictWrong && attempt.Verdict != VerdictFailed {
				return ArmRunArtifact{}, fmt.Errorf("attempt %d has invalid verdict %q", sourceIndex, attempt.Verdict)
			}
			if err := validateAttemptTrace(sourceIndex, attempt, manifest.Budget); err != nil {
				return ArmRunArtifact{}, err
			}

			question := attempt.Question
			if strings.TrimSpace(question) == "" {
				question, err = QuestionFor(scenario.Name)
				if err != nil {
					return ArmRunArtifact{}, err
				}
			}
			record, err := NewAttemptRecord(question, scenario, attempt)
			if err != nil {
				return ArmRunArtifact{}, fmt.Errorf("attempt %d: %w", sourceIndex, err)
			}
			records = append(records, record)
		}
	}

	return ArmRunArtifact{Manifest: manifest, Path: path, Attempts: records}, nil
}

// NewAttemptRecord creates the self-contained evidence written both to the
// incremental JSONL journal and to the final arm artifact.
func NewAttemptRecord(question string, scenario scenarios.Scenario, attempt Attempt) (AttemptRecord, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return AttemptRecord{}, fmt.Errorf("question is required")
	}
	if strings.TrimSpace(attempt.Scenario) == "" || attempt.Repetition < 1 || attempt.Index < 1 {
		return AttemptRecord{}, fmt.Errorf("attempt identity is incomplete")
	}
	if err := validateToolTrace(attempt.ToolTrace, attempt.ToolCalls); err != nil {
		return AttemptRecord{}, err
	}
	record := AttemptRecord{
		Path:              attempt.Path,
		Scenario:          attempt.Scenario,
		Repetition:        attempt.Repetition,
		Index:             attempt.Index,
		Question:          question,
		Prompt:            attempt.Prompt,
		StartedAt:         attempt.StartedAt,
		DurationMS:        attempt.DurationMS,
		SQL:               attempt.SQL,
		Parameters:        slices.Clone(attempt.Parameters),
		SemanticQuery:     attempt.SemanticQuery,
		ToolCalls:         attempt.ToolCalls,
		ContextTokens:     attempt.ContextTokens,
		OutputTokens:      attempt.OutputTokens,
		Transcript:        attempt.Transcript,
		PersistTranscript: IsToolBudgetExceededError(attempt.Err),
		ToolTrace:         append([]ToolCallEvidence(nil), attempt.ToolTrace...),
		Result:            attempt.Result,
		Expected:          scenario.ExpectedResult,
		Trace:             append([]AttemptEvidence(nil), attempt.Trace...),
		Verdict:           attempt.Verdict,
		Reason:            attempt.Reason,
	}
	if attempt.Err != nil {
		record.Error = attempt.Err.Error()
	}
	return record, nil
}

func validateAttemptTrace(recordIndex int, attempt Attempt, budget TaskBudget) error {
	if len(attempt.Trace) != attempt.Index {
		return fmt.Errorf("attempt %d has %d trace entries for final retry index %d; raw retry evidence is incomplete", recordIndex, len(attempt.Trace), attempt.Index)
	}
	totalToolCalls := 0
	for i, evidence := range attempt.Trace {
		wantIndex := i + 1
		if evidence.Index != wantIndex {
			return fmt.Errorf("attempt %d trace entry %d reports index %d, want %d", recordIndex, i, evidence.Index, wantIndex)
		}
		if evidence.ToolCalls < 0 {
			return fmt.Errorf("attempt %d trace entry %d reports %d tool calls outside budget 0..%d", recordIndex, i, evidence.ToolCalls, budget.ToolCalls)
		}
		if evidence.ToolCalls > budget.ToolCalls && (i != len(attempt.Trace)-1 || !validTerminalToolBudgetOverrun(attempt, budget) || evidence.ToolCalls != attempt.ToolCalls) {
			return fmt.Errorf("attempt %d trace entry %d reports %d tool calls outside budget 0..%d without a typed terminal overrun", recordIndex, i, evidence.ToolCalls, budget.ToolCalls)
		}
		if evidence.ContextTokens < 0 || evidence.OutputTokens < 0 {
			return fmt.Errorf("attempt %d trace entry %d reports negative token usage", recordIndex, i)
		}
		if err := validateToolTrace(evidence.ToolTrace, evidence.ToolCalls); err != nil {
			return fmt.Errorf("attempt %d trace entry %d: %w", recordIndex, i, err)
		}
		totalToolCalls += evidence.ToolCalls
	}
	if totalToolCalls > budget.ToolCalls && !validTerminalToolBudgetOverrun(attempt, budget) {
		return fmt.Errorf("attempt %d trace reports %d cumulative tool calls outside shared budget 0..%d without a typed terminal overrun", recordIndex, totalToolCalls, budget.ToolCalls)
	}

	final := attempt.Trace[len(attempt.Trace)-1]
	if final.Prompt != attempt.Prompt || final.SQL != attempt.SQL || !reflect.DeepEqual(final.Parameters, attempt.Parameters) || final.SemanticQuery != attempt.SemanticQuery || final.ToolCalls != attempt.ToolCalls || final.ContextTokens != attempt.ContextTokens || final.OutputTokens != attempt.OutputTokens || final.Transcript != attempt.Transcript || !reflect.DeepEqual(final.ToolTrace, attempt.ToolTrace) || !reflect.DeepEqual(final.Result, attempt.Result) {
		return fmt.Errorf("attempt %d final trace entry does not match the collected final attempt", recordIndex)
	}
	wantError := ""
	if attempt.Err != nil {
		wantError = attempt.Err.Error()
	}
	if final.Error != wantError {
		return fmt.Errorf("attempt %d final trace error %q does not match final error %q", recordIndex, final.Error, wantError)
	}
	return nil
}

func validTerminalToolBudgetOverrun(attempt Attempt, budget TaskBudget) bool {
	// New drivers normalize the first observed over-budget call to budget+1.
	// Older journals can contain a larger count when multiple structured events
	// were already buffered before SIGTERM took effect. Preserve those completed
	// experiments only when the terminal failure is explicitly typed and its
	// exact used/budget evidence matches the persisted attempt.
	used := attempt.ToolCalls
	if len(attempt.Trace) > 0 {
		used = 0
		for _, evidence := range attempt.Trace {
			used += evidence.ToolCalls
		}
	}
	if used <= budget.ToolCalls || attempt.Verdict != VerdictFailed {
		return false
	}
	var exceeded *ToolBudgetExceededError
	return errors.As(attempt.Err, &exceeded) && exceeded.Used == used && exceeded.Budget == budget.ToolCalls
}

func validateToolTrace(trace []ToolCallEvidence, totalToolCalls int) error {
	if len(trace) > totalToolCalls {
		return fmt.Errorf("semantic tool trace has %d entries but total tool calls is %d", len(trace), totalToolCalls)
	}
	for i, entry := range trace {
		if strings.TrimSpace(entry.Name) == "" || entry.DurationMS < 0 || entry.RequestBytes < 0 || entry.ResponseBytes < 0 {
			return fmt.Errorf("semantic tool trace entry %d is invalid", i)
		}
		if len(entry.Error) > 4096 {
			return fmt.Errorf("semantic tool trace entry %d error summary exceeds 4096 bytes", i)
		}
		if len(entry.Arguments) > 4096 {
			return fmt.Errorf("semantic tool trace entry %d argument summary exceeds 4096 bytes", i)
		}
		switch entry.Status {
		case "success", "error", "incomplete":
		default:
			return fmt.Errorf("semantic tool trace entry %d has invalid status %q", i, entry.Status)
		}
		if entry.Status == "success" && entry.Error != "" {
			return fmt.Errorf("semantic tool trace entry %d reports an error for successful status", i)
		}
	}
	return nil
}

// WriteArmRunJSON writes a complete raw artifact. Reports are deliberately not
// written here: they are derived evidence and can always be regenerated from
// this record, while the reverse is impossible.
func WriteArmRunJSON(w io.Writer, artifact ArmRunArtifact) error {
	if w == nil {
		return fmt.Errorf("artifact writer is required")
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(artifact); err != nil {
		return fmt.Errorf("encode S2SBench artifact: %w", err)
	}
	return nil
}
