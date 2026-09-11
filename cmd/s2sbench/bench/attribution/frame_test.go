package attribution

import (
	"reflect"
	"strings"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

func testIdentity() s2sbench.AgentIdentity {
	return s2sbench.AgentIdentity{Name: "codex", Version: "test", Model: "test-model"}
}

func testManifest(t *testing.T, mode RunMode) RunManifest {
	t.Helper()
	manifest, err := NewRunManifest(testIdentity(), "openai", "test-model", "test-model-2026-08", mode, "")
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func completeAttempts(manifest RunManifest, arm Arm) []Attempt {
	var attempts []Attempt
	for _, scenario := range manifest.Scenarios {
		for repetition := manifest.Budget.Repetitions; repetition >= 1; repetition-- {
			attempts = append(attempts, Attempt{
				Arm:               arm,
				Scenario:          scenario,
				Repetition:        repetition,
				AttemptIndex:      1,
				DurationMS:        10,
				ToolCalls:         2,
				ContextTokens:     100,
				OutputTokens:      20,
				Verdict:           s2sbench.VerdictCorrect,
				AnswerFingerprint: scenario + "-stable",
			})
		}
	}
	return attempts
}

func TestManifestKeepsAttributionFrameSeparateAndFrozen(t *testing.T) {
	manifest := testManifest(t, RunModeBenchmark)
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.PromptVersion != PromptVersion || manifest.Budget != FrozenBudget {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Scenarios) != 4 || !reflect.DeepEqual(Scenarios()[0].DimensionRefs, []string{"region", "channel"}) {
		t.Fatalf("frozen scenarios = %#v", Scenarios())
	}

	smoke, err := NewRunManifest(testIdentity(), "openai", "test-model", "test-model-2026-08", RunModeSmoke, "ratio_mix_rate_entry_exit")
	if err != nil {
		t.Fatal(err)
	}
	if len(smoke.Scenarios) != 1 || smoke.Budget.Repetitions != 1 || smoke.Budget.Attempts != 1 {
		t.Fatalf("smoke manifest = %#v", smoke)
	}

	if _, err := NewRunManifest(testIdentity(), "openai", "test-model", "test-model-2026-08", RunModeSmoke, "unknown"); err == nil || !strings.Contains(err.Error(), "unknown attribution scenario") {
		t.Fatalf("unknown smoke scenario error = %v", err)
	}
}

func TestArmArtifactOwnsCanonicalCompleteEvidence(t *testing.T) {
	manifest := testManifest(t, RunModeDiagnostic)
	attempts := completeAttempts(manifest, ArmCompileLoop)
	reverse := append([]Attempt(nil), attempts...)
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}
	artifact, err := NewArmArtifact(manifest, ArmCompileLoop, reverse)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Attempts[0].Scenario != manifest.Scenarios[0] {
		t.Fatalf("canonical attempts = %#v", artifact.Attempts)
	}
	reverse[0].AnswerFingerprint = "mutated"
	if artifact.Attempts[len(artifact.Attempts)-1].AnswerFingerprint == "mutated" {
		t.Fatal("artifact retained caller-owned attempt storage")
	}

	bad := completeAttempts(manifest, ArmCompileLoop)
	bad[0].ReconciliationFailures = 1
	if _, err := NewArmArtifact(manifest, ArmCompileLoop, bad); err == nil || !strings.Contains(err.Error(), "cannot be correct") {
		t.Fatalf("reconciled correctness error = %v", err)
	}
	bad = completeAttempts(manifest, ArmCompileLoop)
	bad[0].AnswerFingerprint = ""
	if _, err := NewArmArtifact(manifest, ArmCompileLoop, bad); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("missing final-answer fingerprint error = %v", err)
	}

	if _, err := NewArmArtifact(manifest, ArmCompileLoop, attempts[:len(attempts)-1]); err == nil || !strings.Contains(err.Error(), "want") {
		t.Fatalf("incomplete artifact error = %v", err)
	}

	overrun := completeAttempts(manifest, ArmCompileLoop)
	overrun[0].Verdict = s2sbench.VerdictFailed
	overrun[0].AnswerFingerprint = ""
	overrun[0].ToolCalls = manifest.Budget.ToolCalls + 1
	overrun[0].ToolBudgetExceeded = true
	if _, err := NewArmArtifact(manifest, ArmCompileLoop, overrun); err != nil {
		t.Fatalf("terminal overrun should remain failed evidence: %v", err)
	}
	overrun[0].ToolBudgetExceeded = false
	if _, err := NewArmArtifact(manifest, ArmCompileLoop, overrun); err == nil || !strings.Contains(err.Error(), "outside budget") {
		t.Fatalf("unmarked overrun error = %v", err)
	}
}

func TestReportDerivesRequiredPhaseFMeasures(t *testing.T) {
	manifest := testManifest(t, RunModeBenchmark)
	compileAttempts := completeAttempts(manifest, ArmCompileLoop)
	bundleAttempts := completeAttempts(manifest, ArmAttributionBundle)

	compileAttempts[0].Verdict = s2sbench.VerdictWrong
	compileAttempts[0].ReconciliationFailures = 2
	compileAttempts[0].AnswerFingerprint = "different"
	compileAttempts[1].Verdict = s2sbench.VerdictFailed
	compileAttempts[1].AnswerFingerprint = ""
	compileAttempts[2].AttemptIndex = 2
	compileAttempts[2].ToolCalls = 4
	compileAttempts[2].ContextTokens = 150
	compileAttempts[2].OutputTokens = 30

	compileArtifact, err := NewArmArtifact(manifest, ArmCompileLoop, compileAttempts)
	if err != nil {
		t.Fatal(err)
	}
	bundleArtifact, err := NewArmArtifact(manifest, ArmAttributionBundle, bundleAttempts)
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildReport(compileArtifact, bundleArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if report.CompileLoop.Total != 20 || report.CompileLoop.Correct != 18 || report.CompileLoop.Wrong != 1 || report.CompileLoop.Failed != 1 || report.CompileLoop.FirstTryCorrect != 17 {
		t.Fatalf("compile-loop outcome summary = %#v", report.CompileLoop)
	}
	if report.CompileLoop.AccuracyPercent != 90 || report.CompileLoop.ReconciliationFailures != 2 {
		t.Fatalf("compile-loop correctness evidence = %#v", report.CompileLoop)
	}
	if report.CompileLoop.ToolCalls != 42 || report.CompileLoop.ContextTokens != 2050 || report.CompileLoop.OutputTokens != 410 {
		t.Fatalf("compile-loop usage = %#v", report.CompileLoop)
	}
	if !reflect.DeepEqual(report.CompileLoop.InconsistentScenarios, []string{manifest.Scenarios[0]}) {
		t.Fatalf("compile-loop consistency = %#v", report.CompileLoop.InconsistentScenarios)
	}
	if report.AttributionBundle.Correct != 20 || report.AttributionBundle.AccuracyPercent != 100 || report.AttributionBundle.ReconciliationFailures != 0 || len(report.AttributionBundle.InconsistentScenarios) != 0 {
		t.Fatalf("bundle summary = %#v", report.AttributionBundle)
	}
}

func TestReportRejectsNonComparableArms(t *testing.T) {
	manifest := testManifest(t, RunModeDiagnostic)
	left, err := NewArmArtifact(manifest, ArmCompileLoop, completeAttempts(manifest, ArmCompileLoop))
	if err != nil {
		t.Fatal(err)
	}
	rightManifest := manifest
	rightManifest.ModelVersion = "different"
	right, err := NewArmArtifact(rightManifest, ArmAttributionBundle, completeAttempts(rightManifest, ArmAttributionBundle))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildReport(left, right); err == nil || !strings.Contains(err.Error(), "identical manifest") {
		t.Fatalf("manifest mismatch error = %v", err)
	}
}
