package s2sbench

import (
	"strings"
	"testing"
)

func TestArmRunArtifactAcceptsLegacyBufferedTypedTerminalOverrun(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-27")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	attempts := completeAttempts(t, PathMetis, manifest.Budget.Repetitions)
	used := manifest.Budget.ToolCalls + 2
	attempts[0].ToolCalls = used
	attempts[0].Verdict = VerdictFailed
	attempts[0].Err = NewToolBudgetExceededError(used, manifest.Budget.ToolCalls)
	attempts[0].Reason = attempts[0].Err.Error()
	attempts[0].Trace[0].ToolCalls = used
	attempts[0].Trace[0].Error = attempts[0].Err.Error()

	if _, err := NewArmRunArtifact(manifest, PathMetis, attempts); err != nil {
		t.Fatalf("NewArmRunArtifact rejected typed buffered terminal overrun: %v", err)
	}

	attempts[0].Err = nil
	attempts[0].Reason = ""
	attempts[0].Trace[0].Error = ""
	if _, err := NewArmRunArtifact(manifest, PathMetis, attempts); err == nil || !strings.Contains(err.Error(), "typed terminal overrun") {
		t.Fatalf("error = %v, want untyped overrun refusal", err)
	}
}
