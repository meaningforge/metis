// Package attribution defines the isolated Agent evaluation frame.
// It does not register tools or expose a production application surface.
package attribution

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

const (
	ManifestSchemaVersion = "agent-attribution-v0.1"
	PromptVersion         = "v0.1"
)

type Arm string

const (
	ArmCompileLoop       Arm = "compile_loop"
	ArmAttributionBundle Arm = "attribution_bundle"
)

var Arms = []Arm{ArmCompileLoop, ArmAttributionBundle}

type RunMode string

const (
	RunModeBenchmark  RunMode = "benchmark"
	RunModeDiagnostic RunMode = "diagnostic"
	RunModeSmoke      RunMode = "smoke"
)

var FrozenBudget = s2sbench.TaskBudget{ToolCalls: 12, Attempts: 2, Repetitions: 5}

type Scenario struct {
	Name          string
	Question      string
	Decomposition DecompositionKind
	DimensionRefs []string
}

type DecompositionKind string

const (
	DecompositionAdditive DecompositionKind = "additive"
	DecompositionRatio    DecompositionKind = "ratio"
)

var frozenScenarios = []Scenario{
	{
		Name:          "additive_population_union",
		Question:      "Why did revenue change between July and August 2026? Show independently reconciled evidence by region and by channel.",
		Decomposition: DecompositionAdditive,
		DimensionRefs: []string{"region", "channel"},
	},
	{
		Name:          "additive_offsetting_zero_total",
		Question:      "Which customer segments offset one another when revenue was unchanged between July and August 2026?",
		Decomposition: DecompositionAdditive,
		DimensionRefs: []string{"segment"},
	},
	{
		Name:          "ratio_mix_rate_entry_exit",
		Question:      "Why did conversion rate change between July and August 2026? Separate segment rate, mix, entry, and exit evidence.",
		Decomposition: DecompositionRatio,
		DimensionRefs: []string{"segment"},
	},
	{
		Name:          "ratio_undefined_denominator",
		Question:      "Explain the conversion-rate change between July and August 2026, including any segments or totals for which the rate is undefined.",
		Decomposition: DecompositionRatio,
		DimensionRefs: []string{"segment"},
	},
}

func Scenarios() []Scenario {
	out := make([]Scenario, len(frozenScenarios))
	for i, scenario := range frozenScenarios {
		out[i] = scenario
		out[i].DimensionRefs = append([]string(nil), scenario.DimensionRefs...)
	}
	return out
}

func ScenarioByName(name string) (Scenario, error) {
	for _, scenario := range Scenarios() {
		if scenario.Name == name {
			return scenario, nil
		}
	}
	return Scenario{}, fmt.Errorf("unknown attribution scenario %q", name)
}

type RunManifest struct {
	SchemaVersion string                 `json:"schema_version"`
	PromptVersion string                 `json:"prompt_version"`
	RunMode       RunMode                `json:"run_mode"`
	Agent         s2sbench.AgentIdentity `json:"agent"`
	ModelProvider string                 `json:"model_provider"`
	ModelID       string                 `json:"model_id"`
	ModelVersion  string                 `json:"model_version"`
	Budget        s2sbench.TaskBudget    `json:"budget"`
	Scenarios     []string               `json:"scenarios"`
}

func NewRunManifest(agent s2sbench.AgentIdentity, provider, modelID, modelVersion string, mode RunMode, smokeScenario string) (RunManifest, error) {
	budget := FrozenBudget
	var names []string
	switch mode {
	case RunModeBenchmark:
	case RunModeDiagnostic:
		budget.Repetitions = 1
	case RunModeSmoke:
		budget.Repetitions = 1
		budget.Attempts = 1
		names = []string{strings.TrimSpace(smokeScenario)}
	default:
		return RunManifest{}, fmt.Errorf("attribution run mode %q is not supported", mode)
	}
	if len(names) == 0 {
		for _, scenario := range frozenScenarios {
			names = append(names, scenario.Name)
		}
	}
	manifest := RunManifest{
		SchemaVersion: ManifestSchemaVersion,
		PromptVersion: PromptVersion,
		RunMode:       mode,
		Agent:         agent,
		ModelProvider: strings.TrimSpace(provider),
		ModelID:       strings.TrimSpace(modelID),
		ModelVersion:  strings.TrimSpace(modelVersion),
		Budget:        budget,
		Scenarios:     names,
	}
	if err := manifest.Validate(); err != nil {
		return RunManifest{}, err
	}
	return manifest, nil
}

func (manifest RunManifest) Validate() error {
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.PromptVersion != PromptVersion {
		return fmt.Errorf("attribution benchmark identity does not match the frozen frame")
	}
	if err := manifest.Agent.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(manifest.ModelProvider) == "" || strings.TrimSpace(manifest.ModelID) == "" || strings.TrimSpace(manifest.ModelVersion) == "" {
		return fmt.Errorf("attribution benchmark provider and model identity are required")
	}
	if strings.TrimSpace(manifest.Agent.Model) != strings.TrimSpace(manifest.ModelID) {
		return fmt.Errorf("agent model %q does not match manifest model %q", manifest.Agent.Model, manifest.ModelID)
	}
	wantBudget := FrozenBudget
	switch manifest.RunMode {
	case RunModeBenchmark:
	case RunModeDiagnostic:
		wantBudget.Repetitions = 1
	case RunModeSmoke:
		wantBudget.Repetitions = 1
		wantBudget.Attempts = 1
	default:
		return fmt.Errorf("attribution run mode %q is not supported", manifest.RunMode)
	}
	if manifest.Budget != wantBudget {
		return fmt.Errorf("attribution run budget %+v, want %+v", manifest.Budget, wantBudget)
	}
	known := make(map[string]struct{}, len(frozenScenarios))
	for _, scenario := range frozenScenarios {
		known[scenario.Name] = struct{}{}
	}
	if manifest.RunMode == RunModeSmoke {
		if len(manifest.Scenarios) != 1 {
			return fmt.Errorf("attribution smoke run requires exactly one scenario")
		}
		if _, ok := known[manifest.Scenarios[0]]; !ok {
			return fmt.Errorf("unknown attribution scenario %q", manifest.Scenarios[0])
		}
		return nil
	}
	if len(manifest.Scenarios) != len(frozenScenarios) {
		return fmt.Errorf("attribution manifest has %d scenarios, want %d", len(manifest.Scenarios), len(frozenScenarios))
	}
	for i, scenario := range frozenScenarios {
		if manifest.Scenarios[i] != scenario.Name {
			return fmt.Errorf("attribution scenario %d is %q, want %q", i, manifest.Scenarios[i], scenario.Name)
		}
	}
	return nil
}

type Attempt struct {
	Arm                    Arm              `json:"arm"`
	Scenario               string           `json:"scenario"`
	Repetition             int              `json:"repetition"`
	AttemptIndex           int              `json:"attempt_index"`
	DurationMS             int64            `json:"duration_ms"`
	ToolCalls              int              `json:"tool_calls"`
	ToolBudgetExceeded     bool             `json:"tool_budget_exceeded,omitempty"`
	ContextTokens          int              `json:"context_tokens"`
	OutputTokens           int              `json:"output_tokens"`
	Verdict                s2sbench.Verdict `json:"verdict"`
	ReconciliationFailures int              `json:"reconciliation_failures"`
	AnswerFingerprint      string           `json:"answer_fingerprint,omitempty"`
}

type ArmArtifact struct {
	Manifest RunManifest `json:"manifest"`
	Arm      Arm         `json:"arm"`
	Attempts []Attempt   `json:"attempts"`
}

func NewArmArtifact(manifest RunManifest, arm Arm, attempts []Attempt) (ArmArtifact, error) {
	if err := manifest.Validate(); err != nil {
		return ArmArtifact{}, err
	}
	if !validArm(arm) {
		return ArmArtifact{}, fmt.Errorf("unknown attribution arm %q", arm)
	}
	type key struct {
		scenario   string
		repetition int
	}
	byKey := make(map[key]Attempt, len(attempts))
	for i, attempt := range attempts {
		if attempt.Arm != arm {
			return ArmArtifact{}, fmt.Errorf("attempt %d arm is %q, want %q", i, attempt.Arm, arm)
		}
		if attempt.Repetition < 1 || attempt.Repetition > manifest.Budget.Repetitions || attempt.AttemptIndex < 1 || attempt.AttemptIndex > manifest.Budget.Attempts {
			return ArmArtifact{}, fmt.Errorf("attempt %d identity is outside the frozen budget", i)
		}
		if attempt.ToolCalls < 0 || attempt.ContextTokens < 0 || attempt.OutputTokens < 0 || attempt.DurationMS < 0 || attempt.ReconciliationFailures < 0 {
			return ArmArtifact{}, fmt.Errorf("attempt %d reports invalid usage or reconciliation evidence", i)
		}
		if attempt.ToolBudgetExceeded {
			if attempt.Verdict != s2sbench.VerdictFailed || attempt.ToolCalls != manifest.Budget.ToolCalls+1 {
				return ArmArtifact{}, fmt.Errorf("attempt %d has invalid terminal tool-budget overrun evidence", i)
			}
		} else if attempt.ToolCalls > manifest.Budget.ToolCalls {
			return ArmArtifact{}, fmt.Errorf("attempt %d reports %d tool calls outside budget 0..%d", i, attempt.ToolCalls, manifest.Budget.ToolCalls)
		}
		if attempt.Verdict != s2sbench.VerdictCorrect && attempt.Verdict != s2sbench.VerdictWrong && attempt.Verdict != s2sbench.VerdictFailed {
			return ArmArtifact{}, fmt.Errorf("attempt %d has invalid verdict %q", i, attempt.Verdict)
		}
		if attempt.Verdict == s2sbench.VerdictCorrect && attempt.ReconciliationFailures != 0 {
			return ArmArtifact{}, fmt.Errorf("attempt %d cannot be correct with reconciliation failures", i)
		}
		if attempt.Verdict != s2sbench.VerdictFailed && strings.TrimSpace(attempt.AnswerFingerprint) == "" {
			return ArmArtifact{}, fmt.Errorf("attempt %d requires a canonical final-answer fingerprint", i)
		}
		identity := key{scenario: attempt.Scenario, repetition: attempt.Repetition}
		if _, exists := byKey[identity]; exists {
			return ArmArtifact{}, fmt.Errorf("duplicate attribution attempt for %s repetition %d", attempt.Scenario, attempt.Repetition)
		}
		byKey[identity] = attempt
	}
	want := len(manifest.Scenarios) * manifest.Budget.Repetitions
	if len(attempts) != want {
		return ArmArtifact{}, fmt.Errorf("attribution arm %q has %d attempts, want %d", arm, len(attempts), want)
	}
	ordered := make([]Attempt, 0, want)
	for _, scenario := range manifest.Scenarios {
		for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
			attempt, ok := byKey[key{scenario: scenario, repetition: repetition}]
			if !ok {
				return ArmArtifact{}, fmt.Errorf("missing attribution attempt for %s repetition %d", scenario, repetition)
			}
			ordered = append(ordered, attempt)
		}
	}
	return ArmArtifact{Manifest: manifest, Arm: arm, Attempts: ordered}, nil
}

type ArmSummary struct {
	Arm                    Arm      `json:"arm"`
	Total                  int      `json:"total"`
	Correct                int      `json:"correct"`
	Wrong                  int      `json:"wrong"`
	Failed                 int      `json:"failed"`
	FirstTryCorrect        int      `json:"first_try_correct"`
	AccuracyPercent        float64  `json:"accuracy_percent"`
	ToolCalls              int      `json:"tool_calls"`
	ContextTokens          int      `json:"context_tokens"`
	OutputTokens           int      `json:"output_tokens"`
	ReconciliationFailures int      `json:"reconciliation_failures"`
	InconsistentScenarios  []string `json:"inconsistent_scenarios"`
}

type Report struct {
	Manifest          RunManifest `json:"manifest"`
	CompileLoop       ArmSummary  `json:"compile_loop"`
	AttributionBundle ArmSummary  `json:"attribution_bundle"`
}

func BuildReport(compileLoop, attributionBundle ArmArtifact) (Report, error) {
	if !reflect.DeepEqual(compileLoop.Manifest, attributionBundle.Manifest) {
		return Report{}, fmt.Errorf("attribution arms do not share the identical manifest")
	}
	if compileLoop.Arm != ArmCompileLoop || attributionBundle.Arm != ArmAttributionBundle {
		return Report{}, fmt.Errorf("attribution report requires compile_loop then attribution_bundle artifacts")
	}
	left, err := summarize(compileLoop)
	if err != nil {
		return Report{}, err
	}
	right, err := summarize(attributionBundle)
	if err != nil {
		return Report{}, err
	}
	return Report{Manifest: compileLoop.Manifest, CompileLoop: left, AttributionBundle: right}, nil
}

func summarize(artifact ArmArtifact) (ArmSummary, error) {
	validated, err := NewArmArtifact(artifact.Manifest, artifact.Arm, artifact.Attempts)
	if err != nil {
		return ArmSummary{}, err
	}
	summary := ArmSummary{Arm: artifact.Arm, Total: len(validated.Attempts)}
	answers := make(map[string]map[string]struct{})
	for _, attempt := range validated.Attempts {
		switch attempt.Verdict {
		case s2sbench.VerdictCorrect:
			summary.Correct++
			if attempt.AttemptIndex == 1 {
				summary.FirstTryCorrect++
			}
		case s2sbench.VerdictWrong:
			summary.Wrong++
		case s2sbench.VerdictFailed:
			summary.Failed++
		}
		summary.ToolCalls += attempt.ToolCalls
		summary.ContextTokens += attempt.ContextTokens
		summary.OutputTokens += attempt.OutputTokens
		summary.ReconciliationFailures += attempt.ReconciliationFailures
		if answers[attempt.Scenario] == nil {
			answers[attempt.Scenario] = map[string]struct{}{}
		}
		signature := string(attempt.Verdict) + ":" + strings.TrimSpace(attempt.AnswerFingerprint)
		answers[attempt.Scenario][signature] = struct{}{}
	}
	if summary.Total > 0 {
		summary.AccuracyPercent = math.Round(float64(summary.Correct)*10000/float64(summary.Total)) / 100
	}
	for scenario, signatures := range answers {
		if len(signatures) > 1 {
			summary.InconsistentScenarios = append(summary.InconsistentScenarios, scenario)
		}
	}
	sort.Strings(summary.InconsistentScenarios)
	return summary, nil
}

func validArm(arm Arm) bool {
	return arm == ArmCompileLoop || arm == ArmAttributionBundle
}
