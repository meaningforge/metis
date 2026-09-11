package readiness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestResultMismatchCategorySeparatesTypeFromValue(t *testing.T) {
	if got := resultMismatchCategory(errors.New(`column 0 is "datetime", want "date" (name "month" is not compared)`)); got != "result_type_mismatch" {
		t.Fatalf("type mismatch category = %q", got)
	}
	if got := resultMismatchCategory(errors.New("row 1 differs")); got != "result_value_mismatch" {
		t.Fatalf("value mismatch category = %q", got)
	}
}

func TestExtractTranscriptFileReadsKeepsOnlyWorkspacePaths(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "knowledge"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "knowledge", "index.md"), []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := fmt.Sprintf(`{"method":"item/completed","params":{"item":{"type":"commandExecution","command":"sed -n '1,40p' knowledge/index.md %s","aggregatedOutput":"12345"}}}`, outside)
	reads := extractTranscriptFileReads([]byte(transcript), workspace, 0)
	if len(reads) != 1 {
		t.Fatalf("reads = %#v, want one workspace read", reads)
	}
	if reads[0].Path != "knowledge/index.md" || reads[0].Operation != "sed" || reads[0].Bytes != 5 || reads[0].Order != 1 {
		t.Fatalf("read evidence = %#v", reads[0])
	}
}

func TestBuildReportAppliesControlHealthGate(t *testing.T) {
	manifest, err := NewSmokeManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	collection := Collection{Manifest: manifest, Attempts: []AttemptRecord{
		{Arm: ArmOKF, Scenario: SmokeScenarios[0].Name, QuestionNumber: 1, Repetition: 1, Attempt: 1, Verdict: "silent_wrong"},
		{Arm: ArmOKF, Scenario: SmokeScenarios[1].Name, QuestionNumber: 2, Repetition: 1, Attempt: 1, Verdict: "correct"},
	}}
	report, err := BuildReport(collection)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatal("control failure should invalidate smoke")
	}
}

func TestBuildReportSeparatesSemanticHandoffAndEndToEndLayers(t *testing.T) {
	manifest, err := NewSmokeManifest(ArmMetisMCP, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	collection := Collection{Manifest: manifest, Attempts: []AttemptRecord{
		{Arm: ArmMetisMCP, Scenario: manifest.Scenarios[0].Name, QuestionNumber: 1, Repetition: 1, Attempt: 1, Verdict: "failed", FailureCategory: "malformed_answer", CompileEvidence: &CompileEvidence{Observed: true, ParseOK: true, ExecutionOK: true, OracleOK: true}},
		{Arm: ArmMetisMCP, Scenario: manifest.Scenarios[0].Name, QuestionNumber: 1, Repetition: 1, Attempt: 2, Verdict: "failed", FailureCategory: "timeout", ParseOK: true, HandoffMatchesCompile: true},
		{Arm: ArmMetisMCP, Scenario: manifest.Scenarios[1].Name, QuestionNumber: 2, Repetition: 1, Attempt: 1, Verdict: "correct", ParseOK: true, ExecutionOK: true, OracleOK: true},
	}}
	report, err := BuildReport(collection)
	if err != nil {
		t.Fatal(err)
	}
	if report.SemanticIntentSuccess != 2 || report.HandoffSuccess != 2 || report.EndToEndSuccess != 1 {
		t.Fatalf("layered report = %#v", report)
	}
}

func TestMetisWorkspaceOmitsPhysicalSchema(t *testing.T) {
	root := t.TempDir()
	if err := materializeWorkspace(root, ArmMetisMCP, "CREATE TABLE secret_physical_shape(id BIGINT)", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "schema.sql")); !os.IsNotExist(err) {
		t.Fatalf("Metis workspace schema stat error = %v", err)
	}
}

func TestBuildReportUsesSeventyPercentControlThreshold(t *testing.T) {
	manifest, err := NewFormalPositiveManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	base := Collection{Manifest: manifest}
	for index, spec := range manifest.Scenarios {
		for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
			base.Attempts = append(base.Attempts, AttemptRecord{Arm: ArmOKF, Scenario: spec.Name, QuestionNumber: index + 1, Repetition: repetition, Attempt: 1, Verdict: "correct"})
		}
	}
	for name, failures := range map[string]int{"five_of_six_is_valid": 1, "four_of_six_is_invalid": 2} {
		t.Run(name, func(t *testing.T) {
			collection := base
			collection.Attempts = append([]AttemptRecord(nil), base.Attempts...)
			failedScenarios := map[string]bool{}
			for index := range collection.Attempts {
				attempt := &collection.Attempts[index]
				for _, spec := range manifest.Scenarios {
					if spec.Name == attempt.Scenario && spec.Stratum == "control" && (failedScenarios[spec.Name] || len(failedScenarios) < failures) {
						attempt.Verdict = "silent_wrong"
						failedScenarios[spec.Name] = true
						break
					}
				}
			}
			report, err := BuildReport(collection)
			if err != nil {
				t.Fatal(err)
			}
			if wantValid := failures == 1; report.Valid != wantValid {
				t.Fatalf("valid=%t, want %t", report.Valid, wantValid)
			}
		})
	}
}

func TestBuildComparisonReportAlignsArmsHorizontallyByQuestionNumber(t *testing.T) {
	okf := completedSmokeCollection(t, ArmOKF)
	metis := completedSmokeCollection(t, ArmMetisMCP)
	metis.Attempts[1].Verdict = "silent_wrong"

	report, err := BuildComparisonReport(okf, metis)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Questions) != 2 || report.Questions[1].QuestionNumber != 2 {
		t.Fatalf("questions = %#v", report.Questions)
	}
	if !report.Questions[1].Arms[ArmOKF].FirstReady || report.Questions[1].Arms[ArmMetisMCP].FirstReady {
		t.Fatalf("question 2 horizontal outcome = %#v", report.Questions[1])
	}
	if report.Pairwise[0].Delta != 0.5 {
		t.Fatalf("delta = %v, want 0.5", report.Pairwise[0].Delta)
	}
}

func TestBuildComparisonReportRejectsFrameMismatch(t *testing.T) {
	okf := completedSmokeCollection(t, ArmOKF)
	metis := completedSmokeCollection(t, ArmMetisMCP)
	metis.Manifest.ModelVersion = "different"
	if _, err := BuildComparisonReport(okf, metis); err == nil {
		t.Fatal("comparison accepted different model versions")
	}
}

func TestBuildComparisonReportRejectsQuestionNumberMismatch(t *testing.T) {
	okf := completedSmokeCollection(t, ArmOKF)
	metis := completedSmokeCollection(t, ArmMetisMCP)
	metis.Attempts[1].QuestionNumber = 1
	if _, err := BuildComparisonReport(okf, metis); err == nil {
		t.Fatal("comparison accepted a misnumbered Metis question")
	}
}

func TestBuildComparisonReportSupportsTwoArms(t *testing.T) {
	report, err := BuildComparisonReport(
		completedSmokeCollection(t, ArmOKF),
		completedSmokeCollection(t, ArmMetisMCP),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ArmOrder) != 2 || len(report.Pairwise) != 1 || len(report.Questions[0].Arms) != 2 {
		t.Fatalf("two-arm report = %#v", report)
	}
}

func completedSmokeCollection(t *testing.T, arm Arm) Collection {
	t.Helper()
	manifest, err := NewSmokeManifest(arm, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	collection := Collection{Manifest: manifest, FactLedgerDigest: "sha256:ledger"}
	for index, spec := range manifest.Scenarios {
		collection.Attempts = append(collection.Attempts, AttemptRecord{Arm: arm, Scenario: spec.Name, QuestionNumber: index + 1, Repetition: 1, Attempt: 1, Verdict: "correct", ParseOK: true, ExecutionOK: true, OracleOK: true})
	}
	return collection
}

func TestCollectFromSkipsCompletedQuestionNumbers(t *testing.T) {
	completed := completedSmokeCollection(t, ArmOKF)
	definitions, err := fixtures.CanonicalSemanticModels()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := BuildCatalogProjection(SourceCatalog{
		SchemaVersion: CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB", Database: "s2sbench", Schema: "analytics",
		Generator: "test fixture", GeneratedAt: "2026-09-02T00:00:00Z",
		Tables: []CatalogTable{{Name: "orders", Columns: []CatalogColumn{{Position: 1, Name: "id", Type: "BIGINT"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collection, report, err := CollectFrom(context.Background(), completed.Manifest, neverOpenDriver{}, neverExecute{}, projection, definitions, completed.Attempts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.Attempts) != len(completed.Attempts) || report.FirstReady != len(completed.Manifest.Scenarios) {
		t.Fatalf("resumed collection attempts=%d first_ready=%d", len(collection.Attempts), report.FirstReady)
	}
}

func TestCollectFromRejectsIncompleteRepairBatch(t *testing.T) {
	collection := completedSmokeCollection(t, ArmOKF)
	collection.Attempts = collection.Attempts[:1]
	collection.Attempts[0].Verdict = "failed"
	collection.Attempts[0].FailureCategory = "malformed_answer"
	if _, err := completedQuestionRepetitions(collection); err == nil {
		t.Fatal("resume accepted an incomplete repair batch")
	}
}

type neverOpenDriver struct{}

func (neverOpenDriver) Identity(context.Context) (s2sbench.AgentIdentity, error) {
	return testIdentity(), nil
}

func (neverOpenDriver) Open(context.Context, s2sbench.AgentRequest) (s2sbench.AgentSession, error) {
	return nil, errors.New("driver must not be called for completed questions")
}

type neverExecute struct{}

func (neverExecute) Prepare(context.Context, scenarios.Scenario) error {
	return errors.New("execution must not be called for completed questions")
}

func (neverExecute) PhysicalSchema(context.Context) (string, error) {
	return "", errors.New("execution must not be called for completed questions")
}

func (neverExecute) RunSQL(context.Context, string, ...sql.QueryParameter) (scenarios.ResultSet, error) {
	return scenarios.ResultSet{}, errors.New("execution must not be called for completed questions")
}
