package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
)

func TestRunAnalyzeReadsArmDirectoriesAndEmitsHorizontalRows(t *testing.T) {
	root := t.TempDir()
	okfDir := writeReadinessCollection(t, root, readiness.ArmOKF)
	metisDir := writeReadinessCollection(t, root, readiness.ArmMetisMCP)
	var stdout bytes.Buffer
	if err := runAnalyze([]string{"--input", okfDir, "--input", metisDir}, &stdout); err != nil {
		t.Fatal(err)
	}
	var report readiness.ComparisonReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Questions) != len(readiness.SmokeScenarios) || report.Questions[0].QuestionNumber != 1 {
		t.Fatalf("comparison questions = %#v", report.Questions)
	}
}

func writeReadinessCollection(t *testing.T, root string, arm readiness.Arm) string {
	t.Helper()
	identity := s2sbench.AgentIdentity{Name: "test-agent", Version: "v1", Model: "model"}
	manifest, err := readiness.NewSmokeManifest(arm, identity, "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	collection := readiness.Collection{Manifest: manifest, FactLedgerDigest: "sha256:ledger"}
	for index, spec := range manifest.Scenarios {
		collection.Attempts = append(collection.Attempts, readiness.AttemptRecord{
			Arm: arm, Scenario: spec.Name, QuestionNumber: index + 1, Repetition: 1, Attempt: 1, Verdict: "correct", ParseOK: true, ExecutionOK: true, OracleOK: true,
		})
	}
	directory := filepath.Join(root, string(arm))
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(collection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "collection.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}
