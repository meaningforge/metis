package s2sbench

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

// ManifestSchemaVersion is the on-disk identity of the v0 benchmark run
// manifest. v0.4 keeps per-repetition operational evidence compact by omitting
// repeated prompts and raw Agent transcripts from persisted artifacts.
const ManifestSchemaVersion = "s2sbench-v0.6"

type RunMode string
type ProjectContextMode string

const (
	RunModeBenchmark         RunMode            = "benchmark"
	RunModeDiagnostic        RunMode            = "diagnostic"
	RunModeSmoke             RunMode            = "smoke"
	ProjectContextConfigured ProjectContextMode = "configured"
	ProjectContextColdStart  ProjectContextMode = "cold_start"
)

// RunManifest records the frozen experimental frame alongside every collected
// result set. A benchmark result without this information is not interpretable
// later and must not be collected.
//
// The budget is stored once rather than once per arm. That makes equal budgets
// structural: there is no representation in which raw_assets and metis can
// silently receive different limits.
type RunManifest struct {
	SchemaVersion      string             `json:"schema_version"`
	RunMode            RunMode            `json:"run_mode"`
	PromptVersion      string             `json:"prompt_version"`
	Agent              AgentIdentity      `json:"agent"`
	ModelProvider      string             `json:"model_provider"`
	ModelID            string             `json:"model_id"`
	ModelVersion       string             `json:"model_version"`
	Budget             TaskBudget         `json:"budget"`
	Scenarios          []string           `json:"scenarios"`
	ProjectContextMode ProjectContextMode `json:"project_context_mode"`
}

// NewRunManifest is retained for deterministic harness tests that do not launch
// an external agent. Live collections must use NewAgentRunManifest so the exact
// agent executable is pinned in the persisted evidence.
func NewRunManifest(provider, modelID, modelVersion string) (RunManifest, error) {
	return newRunManifest(AgentIdentity{Name: "deterministic-harness", Version: "v0", Model: modelID}, provider, modelID, modelVersion, RunModeBenchmark, FrozenBudget, nil)
}

// NewAgentRunManifest snapshots the frozen frame for one exact local agent and
// model. Both arms of a collection share this identity; comparing different
// agents belongs in separate collections.
func NewAgentRunManifest(agent AgentIdentity, provider, modelID, modelVersion string) (RunManifest, error) {
	return newRunManifest(agent, provider, modelID, modelVersion, RunModeBenchmark, FrozenBudget, nil)
}

// NewDiagnosticAgentRunManifest creates an explicitly non-comparable one-pass
// collection. It is intended for validating a live Agent/model/MCP setup before
// paying for the frozen five-repetition benchmark.
func NewDiagnosticAgentRunManifest(agent AgentIdentity, provider, modelID, modelVersion string) (RunManifest, error) {
	budget := FrozenBudget
	budget.Repetitions = 1
	return newRunManifest(agent, provider, modelID, modelVersion, RunModeDiagnostic, budget, nil)
}

// NewSmokeAgentRunManifest creates a two-arm, one-repetition run for exactly
// one frozen scenario. Smoke evidence validates setup and one semantic path;
// it is not comparable to diagnostic or formal collections.
func NewSmokeAgentRunManifest(agent AgentIdentity, provider, modelID, modelVersion, scenarioName string) (RunManifest, error) {
	return NewSmokeAgentRunManifestWithRepairAttempts(agent, provider, modelID, modelVersion, scenarioName, FrozenBudget.Attempts-1)
}

// NewSmokeAgentRunManifestWithRepairAttempts permits a smoke run to disable
// repair while preserving the frozen benchmark default. repairAttempts counts
// retries after the required first attempt, so the supported values are 0 and
// 1 and the persisted TaskBudget.Attempts values are 1 and 2 respectively.
func NewSmokeAgentRunManifestWithRepairAttempts(agent AgentIdentity, provider, modelID, modelVersion, scenarioName string, repairAttempts int) (RunManifest, error) {
	if repairAttempts < 0 || repairAttempts >= FrozenBudget.Attempts {
		return RunManifest{}, fmt.Errorf("smoke repair attempts must be 0 or %d", FrozenBudget.Attempts-1)
	}
	budget := FrozenBudget
	budget.ToolCalls = SmokeToolCalls
	budget.Repetitions = 1
	budget.Attempts = 1 + repairAttempts
	return newRunManifest(agent, provider, modelID, modelVersion, RunModeSmoke, budget, []string{strings.TrimSpace(scenarioName)})
}

func newRunManifest(agent AgentIdentity, provider, modelID, modelVersion string, mode RunMode, budget TaskBudget, scenarioNames []string) (RunManifest, error) {
	manifest := RunManifest{
		SchemaVersion:      ManifestSchemaVersion,
		RunMode:            mode,
		PromptVersion:      PromptVersion,
		Agent:              agent,
		ModelProvider:      strings.TrimSpace(provider),
		ModelID:            strings.TrimSpace(modelID),
		ModelVersion:       strings.TrimSpace(modelVersion),
		Budget:             budget,
		ProjectContextMode: ProjectContextConfigured,
	}

	if len(scenarioNames) > 0 {
		manifest.Scenarios = append([]string(nil), scenarioNames...)
	} else {
		selected, err := SelectedScenarios()
		if err != nil {
			return RunManifest{}, err
		}
		manifest.Scenarios = make([]string, len(selected))
		for i, scenario := range selected {
			manifest.Scenarios[i] = scenario.Name
		}
	}

	if err := manifest.ValidateForCollection(); err != nil {
		return RunManifest{}, err
	}
	return manifest, nil
}

// ValidateForCollection refuses to collect a run whose metadata does not
// describe exactly the currently frozen v0 frame. Historical manifests may be
// read without this check; this is a write-time gate against silently mixing
// results from different experiments.
func (m RunManifest) ValidateForCollection() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("manifest schema %q, want %q", m.SchemaVersion, ManifestSchemaVersion)
	}
	if m.PromptVersion != PromptVersion {
		return fmt.Errorf("prompt version %q, want frozen %q", m.PromptVersion, PromptVersion)
	}
	if err := m.Agent.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(m.ModelProvider) == "" {
		return fmt.Errorf("model provider is required")
	}
	if strings.TrimSpace(m.ModelID) == "" {
		return fmt.Errorf("model identity is required")
	}
	if strings.TrimSpace(m.ModelVersion) == "" {
		return fmt.Errorf("model version is required")
	}
	if strings.TrimSpace(m.Agent.Model) != strings.TrimSpace(m.ModelID) {
		return fmt.Errorf("agent model %q does not match manifest model identity %q", m.Agent.Model, m.ModelID)
	}
	if m.ProjectContextMode != ProjectContextConfigured && m.ProjectContextMode != ProjectContextColdStart {
		return fmt.Errorf("project context mode %q is not supported", m.ProjectContextMode)
	}
	switch m.RunMode {
	case RunModeBenchmark:
		if m.Budget != FrozenBudget {
			return fmt.Errorf("benchmark run budget %+v, want frozen %+v", m.Budget, FrozenBudget)
		}
	case RunModeDiagnostic:
		want := FrozenBudget
		want.Repetitions = 1
		if m.Budget != want {
			return fmt.Errorf("diagnostic run budget %+v, want one-pass %+v", m.Budget, want)
		}
	case RunModeSmoke:
		want := FrozenBudget
		want.ToolCalls = SmokeToolCalls
		want.Repetitions = 1
		if m.Budget.ToolCalls != want.ToolCalls || m.Budget.QuestionTimeoutMS != want.QuestionTimeoutMS || m.Budget.Repetitions != want.Repetitions || m.Budget.Attempts < 1 || m.Budget.Attempts > want.Attempts {
			return fmt.Errorf("smoke run budget %+v, want one-pass budget with 1..%d total attempts, %d tool calls, and %dms question timeout", m.Budget, want.Attempts, want.ToolCalls, want.QuestionTimeoutMS)
		}
	default:
		return fmt.Errorf("run mode %q is not supported", m.RunMode)
	}

	selected, err := SelectedScenarios()
	if err != nil {
		return err
	}
	if m.RunMode == RunModeSmoke {
		if len(m.Scenarios) != 1 {
			return fmt.Errorf("smoke manifest has %d scenarios, want exactly 1", len(m.Scenarios))
		}
		if _, err := ScenariosForManifest(m); err != nil {
			return err
		}
		return nil
	}
	if len(m.Scenarios) != len(selected) {
		return fmt.Errorf("manifest has %d scenarios, frozen selection has %d", len(m.Scenarios), len(selected))
	}
	for i, scenario := range selected {
		if m.Scenarios[i] != scenario.Name {
			return fmt.Errorf("manifest scenario %d is %q, want frozen %q", i, m.Scenarios[i], scenario.Name)
		}
	}
	return nil
}

// ScenariosForManifest resolves the exact ordered scenario set recorded in a
// validated collection manifest. Smoke mode may select one member of the
// frozen sample; no mode may reach outside that sample.
func ScenariosForManifest(m RunManifest) ([]scenarios.Scenario, error) {
	selected, err := SelectedScenarios()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]scenarios.Scenario, len(selected))
	for _, scenario := range selected {
		byName[scenario.Name] = scenario
	}
	resolved := make([]scenarios.Scenario, len(m.Scenarios))
	for i, name := range m.Scenarios {
		scenario, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("manifest scenario %d is %q, which is not in the frozen selection", i, name)
		}
		resolved[i] = scenario
	}
	return resolved, nil
}
