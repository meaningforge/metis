package s2sbench

import (
	"strings"
	"testing"
)

func TestRunManifestPinsTheFrozenFrame(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-24")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	if err := manifest.ValidateForCollection(); err != nil {
		t.Fatalf("ValidateForCollection: %v", err)
	}
	if manifest.PromptVersion != PromptVersion {
		t.Fatalf("prompt version = %q, want %q", manifest.PromptVersion, PromptVersion)
	}
	if manifest.Budget != FrozenBudget {
		t.Fatalf("budget = %+v, want %+v", manifest.Budget, FrozenBudget)
	}
	if manifest.RunMode != RunModeBenchmark {
		t.Fatalf("run mode = %q, want %q", manifest.RunMode, RunModeBenchmark)
	}

	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatalf("SelectedScenarios: %v", err)
	}
	if len(manifest.Scenarios) != len(selected) {
		t.Fatalf("scenario count = %d, want %d", len(manifest.Scenarios), len(selected))
	}
	for i, scenario := range selected {
		if manifest.Scenarios[i] != scenario.Name {
			t.Fatalf("scenario %d = %q, want %q", i, manifest.Scenarios[i], scenario.Name)
		}
	}
}

func TestDiagnosticManifestIsExplicitlyOnePass(t *testing.T) {
	manifest, err := NewDiagnosticAgentRunManifest(
		AgentIdentity{Name: "codex", Version: "test", Model: "model-id"},
		"provider", "model-id", "version",
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RunMode != RunModeDiagnostic || manifest.Budget.Repetitions != 1 || manifest.Budget.QuestionTimeoutMS != 60_000 {
		t.Fatalf("diagnostic manifest mode/budget = %q/%+v", manifest.RunMode, manifest.Budget)
	}
	if err := manifest.ValidateForCollection(); err != nil {
		t.Fatalf("ValidateForCollection: %v", err)
	}
}

func TestSmokeManifestPinsOneFrozenScenario(t *testing.T) {
	const scenarioName = "conversion_rate_by_campaign"
	manifest, err := NewSmokeAgentRunManifest(
		AgentIdentity{Name: "codex", Version: "test", Model: "model-id"},
		"provider", "model-id", "version", scenarioName,
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RunMode != RunModeSmoke || manifest.Budget.Repetitions != 1 || manifest.Budget.ToolCalls != SmokeToolCalls {
		t.Fatalf("smoke manifest mode/budget = %q/%+v", manifest.RunMode, manifest.Budget)
	}
	if len(manifest.Scenarios) != 1 || manifest.Scenarios[0] != scenarioName {
		t.Fatalf("smoke scenarios=%v, want [%s]", manifest.Scenarios, scenarioName)
	}
	resolved, err := ScenariosForManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Name != scenarioName {
		t.Fatalf("resolved smoke scenarios=%v", resolved)
	}
	if _, err := NewSmokeAgentRunManifest(
		AgentIdentity{Name: "codex", Version: "test", Model: "model-id"},
		"provider", "model-id", "version", "not-in-frozen-selection",
	); err == nil {
		t.Fatal("smoke manifest accepted an unknown scenario")
	}
	drifted := manifest
	drifted.Scenarios = append(drifted.Scenarios, "aggregation_variants")
	if err := drifted.ValidateForCollection(); err == nil || !strings.Contains(err.Error(), "exactly 1") {
		t.Fatalf("two-scenario smoke error=%v, want exact-one refusal", err)
	}
}

func TestSmokeManifestCanDisableRepair(t *testing.T) {
	manifest, err := NewSmokeAgentRunManifestWithRepairAttempts(
		AgentIdentity{Name: "codex", Version: "test", Model: "model-id"},
		"provider", "model-id", "version", "aggregation_variants", 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Budget.Attempts != 1 {
		t.Fatalf("total attempts=%d, want exactly one first attempt", manifest.Budget.Attempts)
	}
	if err := manifest.ValidateForCollection(); err != nil {
		t.Fatalf("ValidateForCollection: %v", err)
	}
	if _, err := NewSmokeAgentRunManifestWithRepairAttempts(
		AgentIdentity{Name: "codex", Version: "test", Model: "model-id"},
		"provider", "model-id", "version", "aggregation_variants", 2,
	); err == nil {
		t.Fatal("smoke manifest accepted two repair attempts")
	}
}

func TestRunManifestRefusesUninterpretableOrDriftedRuns(t *testing.T) {
	base, err := NewRunManifest("provider", "model-id", "2026-08-24")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}

	tests := []struct {
		name string
		edit func(*RunManifest)
		want string
	}{
		{name: "provider", edit: func(m *RunManifest) { m.ModelProvider = "" }, want: "provider"},
		{name: "model identity", edit: func(m *RunManifest) { m.ModelID = "" }, want: "identity"},
		{name: "model version", edit: func(m *RunManifest) { m.ModelVersion = "" }, want: "version"},
		{name: "prompt drift", edit: func(m *RunManifest) { m.PromptVersion = "other" }, want: "prompt version"},
		{name: "project context", edit: func(m *RunManifest) { m.ProjectContextMode = "other" }, want: "project context"},
		{name: "budget drift", edit: func(m *RunManifest) { m.Budget.ToolCalls++ }, want: "budget"},
		{name: "selection drift", edit: func(m *RunManifest) { m.Scenarios[0] = "other" }, want: "scenario"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := base
			manifest.Scenarios = append([]string(nil), base.Scenarios...)
			tt.edit(&manifest)
			err := manifest.ValidateForCollection()
			if err == nil {
				t.Fatal("ValidateForCollection accepted drifted manifest")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}
