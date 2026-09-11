package s2sbench

import "testing"

func TestRestoreHistoricalCollectionAcceptsTypedBufferedBudgetOverrun(t *testing.T) {
	manifest, err := NewRunManifest("provider", "model-id", "2026-08-27")
	if err != nil {
		t.Fatal(err)
	}
	manifest.PromptVersion = "v0.20"
	raw := completeAttempts(t, PathRawAssets, manifest.Budget.Repetitions)
	metis := completeAttempts(t, PathMetis, manifest.Budget.Repetitions)
	used := manifest.Budget.ToolCalls + 2
	metis[0].ToolCalls = used
	metis[0].Verdict = VerdictFailed
	metis[0].Err = NewToolBudgetExceededError(used, manifest.Budget.ToolCalls)
	metis[0].Reason = metis[0].Err.Error()
	metis[0].Trace[0].ToolCalls = used
	metis[0].Trace[0].Error = metis[0].Err.Error()

	scenarios, err := ScenariosForManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]int, len(scenarios))
	for i, scenario := range scenarios {
		byName[scenario.Name] = i
	}
	var records []AttemptRecord
	for i := range raw {
		question, err := QuestionFor(raw[i].Scenario)
		if err != nil {
			t.Fatalf("question for record %d: %v", i, err)
		}
		rawRecord, err := NewAttemptRecord(question, scenarios[byName[raw[i].Scenario]], raw[i])
		if err != nil {
			t.Fatalf("raw record %d: %v", i, err)
		}
		metisRecord, err := NewAttemptRecord(question, scenarios[byName[metis[i].Scenario]], metis[i])
		if err != nil {
			t.Fatalf("metis record %d: %v", i, err)
		}
		records = append(records, rawRecord, metisRecord)
	}

	collection, err := RestoreHistoricalCollection(manifest, records)
	if err != nil {
		t.Fatalf("RestoreHistoricalCollection: %v", err)
	}
	if collection.Metis.Manifest.PromptVersion != "v0.20" || collection.RawAssets.Manifest.PromptVersion != "v0.20" {
		t.Fatalf("historical prompt version was not preserved")
	}
	report, err := BuildHistoricalV0Report(collection)
	if err != nil {
		t.Fatalf("BuildHistoricalV0Report: %v", err)
	}
	if report.Manifest.PromptVersion != "v0.20" {
		t.Fatalf("report prompt version = %q, want v0.20", report.Manifest.PromptVersion)
	}
}

func TestRestoreAttemptRecordDoesNotInventTypedBudgetError(t *testing.T) {
	budget := FrozenBudget
	record := AttemptRecord{
		Path:       PathMetis,
		Scenario:   "x",
		Repetition: 1,
		Index:      1,
		ToolCalls:  budget.ToolCalls + 2,
		Verdict:    VerdictFailed,
		Error:      "some unrelated failure",
	}
	attempt, err := RestoreAttemptRecord(record, budget)
	if err != nil {
		t.Fatal(err)
	}
	if IsToolBudgetExceededError(attempt.Err) {
		t.Fatalf("unrelated persisted error was rehydrated as tool-budget evidence")
	}
}
