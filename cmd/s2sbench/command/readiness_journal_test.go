package command

import (
	"path/filepath"
	"strings"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
)

func TestReadinessJournalResumesOnlyIdenticalManifestAndLedger(t *testing.T) {
	output := filepath.Join(t.TempDir(), "readiness")
	identity := s2sbench.AgentIdentity{Name: "test-agent", Version: "v1", Model: "model"}
	manifest, err := readiness.NewSmokeManifest(readiness.ArmOKF, identity, "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	ledger := readiness.Ledger{SchemaVersion: "test", Digest: "sha256:ledger"}
	journal, err := newReadinessJournal(output, manifest, ledger)
	if err != nil {
		t.Fatal(err)
	}
	record := readiness.AttemptRecord{Arm: readiness.ArmOKF, Scenario: manifest.Scenarios[0].Name, QuestionNumber: 1, Repetition: 1, Attempt: 1, Verdict: "correct"}
	if err := journal.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, records, err := resumeReadinessJournal(output, manifest, ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].QuestionNumber != 1 {
		t.Fatalf("resumed records = %#v", records)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	drifted := manifest
	drifted.ModelVersion = "different"
	if _, _, err := resumeReadinessJournal(output, drifted, ledger); err == nil || !strings.Contains(err.Error(), "manifest differs") {
		t.Fatalf("manifest drift error = %v", err)
	}
	driftedLedger := ledger
	driftedLedger.Digest = "sha256:different"
	if _, _, err := resumeReadinessJournal(output, manifest, driftedLedger); err == nil || !strings.Contains(err.Error(), "fact ledger differs") {
		t.Fatalf("ledger drift error = %v", err)
	}
}
