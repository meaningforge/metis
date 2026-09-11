package s2sbench

import (
	"bytes"
	"strings"
	"testing"
)

func TestArmRunArtifactRequiresCompleteFrozenRun(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-24")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	attempts := completeAttempts(t, PathRawAssets, manifest.Budget.Repetitions)
	attempts[0].ContextTokens = 123
	attempts[0].OutputTokens = 45
	attempts[0].Prompt = "large repeated prompt"
	attempts[0].Transcript = `{"type":"large-agent-event"}`
	attempts[0].Trace[0].ContextTokens = 123
	attempts[0].Trace[0].OutputTokens = 45
	attempts[0].Trace[0].Prompt = attempts[0].Prompt
	attempts[0].Trace[0].Transcript = attempts[0].Transcript
	attempts[0].ToolCalls = 1
	attempts[0].ToolTrace = []ToolCallEvidence{{Name: "metis.list_metrics", DurationMS: 12, RequestBytes: 42, ResponseBytes: 84, Status: "success"}}
	attempts[0].Trace[0].ToolCalls = 1
	attempts[0].Trace[0].ToolTrace = append([]ToolCallEvidence(nil), attempts[0].ToolTrace...)

	artifact, err := NewArmRunArtifact(manifest, PathRawAssets, attempts)
	if err != nil {
		t.Fatalf("NewArmRunArtifact: %v", err)
	}
	if len(artifact.Attempts) != len(attempts) {
		t.Fatalf("artifact has %d attempts, want %d", len(artifact.Attempts), len(attempts))
	}

	var encoded bytes.Buffer
	if err := WriteArmRunJSON(&encoded, artifact); err != nil {
		t.Fatalf("WriteArmRunJSON: %v", err)
	}
	for _, want := range []string{
		`"schema_version"`,
		`"model_id": "model-id"`,
		`"path": "raw_assets"`,
		`"attempts"`,
		`"trace"`,
		`"context_tokens": 123`,
		`"output_tokens": 45`,
		`"tool_trace"`,
		`"request_bytes": 42`,
	} {
		if !strings.Contains(encoded.String(), want) {
			t.Fatalf("encoded artifact does not contain %q", want)
		}
	}
	for _, omitted := range []string{`"prompt"`, `"transcript"`} {
		if strings.Contains(encoded.String(), omitted) {
			t.Fatalf("encoded compact artifact contains verbose field %s", omitted)
		}
	}
	for _, omittedValue := range []string{"large repeated prompt", "large-agent-event"} {
		if strings.Contains(encoded.String(), omittedValue) {
			t.Fatalf("encoded compact artifact contains verbose value %q", omittedValue)
		}
	}
}

func TestArmRunArtifactPersistsTheFailedFirstTryAfterRepair(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-24")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	attempts := completeAttempts(t, PathRawAssets, manifest.Budget.Repetitions)
	attempts[0].Index = 2
	attempts[0].SQL = "SELECT repaired"
	attempts[0].ToolCalls = 2
	attempts[0].ContextTokens = 20
	attempts[0].OutputTokens = 5
	attempts[0].Trace = []AttemptEvidence{
		{Index: 1, SQL: "SELECT broken", ToolCalls: 1, ContextTokens: 10, OutputTokens: 3, Error: "syntax error near broken"},
		{Index: 2, SQL: "SELECT repaired", ToolCalls: 2, ContextTokens: 20, OutputTokens: 5, Result: attempts[0].Result},
	}

	artifact, err := NewArmRunArtifact(manifest, PathRawAssets, attempts)
	if err != nil {
		t.Fatalf("NewArmRunArtifact: %v", err)
	}
	var encoded bytes.Buffer
	if err := WriteArmRunJSON(&encoded, artifact); err != nil {
		t.Fatalf("WriteArmRunJSON: %v", err)
	}
	for _, want := range []string{"SELECT broken", "syntax error near broken", "SELECT repaired"} {
		if !strings.Contains(encoded.String(), want) {
			t.Fatalf("encoded artifact lost retry evidence %q", want)
		}
	}
}

func TestArmRunArtifactAcceptsExactlyOneTerminalToolBudgetOverrun(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-24")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	attempts := completeAttempts(t, PathMetis, manifest.Budget.Repetitions)
	overrun := manifest.Budget.ToolCalls + 1
	attempts[0].ToolCalls = overrun
	attempts[0].Verdict = VerdictFailed
	attempts[0].Err = NewToolBudgetExceededError(overrun, manifest.Budget.ToolCalls)
	attempts[0].Reason = attempts[0].Err.Error()
	attempts[0].Trace[0].ToolCalls = overrun
	attempts[0].Trace[0].Error = attempts[0].Err.Error()

	if _, err := NewArmRunArtifact(manifest, PathMetis, attempts); err != nil {
		t.Fatalf("NewArmRunArtifact rejected terminal overrun: %v", err)
	}

	attempts[0].ToolCalls++
	attempts[0].Trace[0].ToolCalls++
	if _, err := NewArmRunArtifact(manifest, PathMetis, attempts); err == nil || !strings.Contains(err.Error(), "without a typed terminal overrun") {
		t.Fatalf("error = %v, want multi-call overrun refusal", err)
	}
}

func TestArmRunArtifactRefusesPartialOrMislabelledEvidence(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-24")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	base := completeAttempts(t, PathMetis, manifest.Budget.Repetitions)

	t.Run("partial", func(t *testing.T) {
		_, err := NewArmRunArtifact(manifest, PathMetis, base[:len(base)-1])
		if err == nil || !strings.Contains(err.Error(), "complete frozen run") {
			t.Fatalf("error = %v, want completeness refusal", err)
		}
	})

	t.Run("wrong path", func(t *testing.T) {
		attempts := append([]Attempt(nil), base...)
		attempts[0].Path = PathRawAssets
		_, err := NewArmRunArtifact(manifest, PathMetis, attempts)
		if err == nil || !strings.Contains(err.Error(), "path") {
			t.Fatalf("error = %v, want path refusal", err)
		}
	})

	t.Run("wrong repetition", func(t *testing.T) {
		attempts := append([]Attempt(nil), base...)
		attempts[0].Repetition = 2
		_, err := NewArmRunArtifact(manifest, PathMetis, attempts)
		if err == nil || !strings.Contains(err.Error(), "repetition") {
			t.Fatalf("error = %v, want repetition refusal", err)
		}
	})

	t.Run("negative token usage", func(t *testing.T) {
		attempts := append([]Attempt(nil), base...)
		attempts[0].ContextTokens = -1
		_, err := NewArmRunArtifact(manifest, PathMetis, attempts)
		if err == nil || !strings.Contains(err.Error(), "negative token usage") {
			t.Fatalf("error = %v, want token-usage refusal", err)
		}
	})

	t.Run("missing retry trace", func(t *testing.T) {
		attempts := append([]Attempt(nil), base...)
		attempts[0].Trace = nil
		_, err := NewArmRunArtifact(manifest, PathMetis, attempts)
		if err == nil || !strings.Contains(err.Error(), "raw retry evidence is incomplete") {
			t.Fatalf("error = %v, want missing-trace refusal", err)
		}
	})
}

func completeAttempts(t *testing.T, path Path, repetitions int) []Attempt {
	t.Helper()
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatalf("SelectedScenarios: %v", err)
	}
	attempts := make([]Attempt, 0, len(selected)*repetitions)
	for _, scenario := range selected {
		for repetition := 1; repetition <= repetitions; repetition++ {
			attempt := Attempt{
				Path:       path,
				Scenario:   scenario.Name,
				Repetition: repetition,
				Index:      1,
				Verdict:    VerdictCorrect,
			}
			attempt.Trace = []AttemptEvidence{{Index: 1, Result: attempt.Result}}
			attempts = append(attempts, attempt)
		}
	}
	return attempts
}
