package s2sbench

import (
	"context"
	"reflect"
	"testing"
)

func TestCollectBuildsCompleteComparableArtifactsForBothArms(t *testing.T) {
	manifest, err := NewRunManifest("deterministic-test", "fake-model", "v0")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}

	collection, err := Collect(
		context.Background(),
		manifest,
		deterministicSQLRunner{path: PathRawAssets},
		&deterministicExecution{},
		deterministicSQLRunner{path: PathMetis},
		&deterministicExecution{},
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	want := len(manifest.Scenarios) * manifest.Budget.Repetitions
	if got := len(collection.RawAssets.Attempts); got != want {
		t.Fatalf("raw-assets attempts = %d, want %d", got, want)
	}
	if got := len(collection.Metis.Attempts); got != want {
		t.Fatalf("Metis attempts = %d, want %d", got, want)
	}
	if !reflect.DeepEqual(collection.RawAssets.Manifest, collection.Metis.Manifest) {
		t.Fatal("the two arms did not retain the identical frozen manifest")
	}

	for i := 0; i < want; i++ {
		raw := collection.RawAssets.Attempts[i]
		metis := collection.Metis.Attempts[i]
		if raw.Path != PathRawAssets || metis.Path != PathMetis {
			t.Fatalf("record %d paths = %q/%q", i, raw.Path, metis.Path)
		}
		if raw.Scenario != metis.Scenario || raw.Repetition != metis.Repetition {
			t.Fatalf("record %d arms are not aligned: raw=%s/%d metis=%s/%d", i, raw.Scenario, raw.Repetition, metis.Scenario, metis.Repetition)
		}
		if raw.Verdict != VerdictCorrect || metis.Verdict != VerdictCorrect {
			t.Fatalf("record %d verdicts = %q/%q", i, raw.Verdict, metis.Verdict)
		}
		if len(raw.Trace) != 1 || len(metis.Trace) != 1 {
			t.Fatalf("record %d trace lengths = %d/%d, want 1/1", i, len(raw.Trace), len(metis.Trace))
		}
		if metis.SemanticQuery == "" {
			t.Fatalf("record %d Metis arm lost semantic-query evidence", i)
		}
	}
}

func TestCollectObservesRawMetisPairsInIdenticalQuestionOrder(t *testing.T) {
	manifest, err := NewDiagnosticAgentRunManifest(
		AgentIdentity{Name: "test-agent", Version: "v1", Model: "fake-model"},
		"deterministic-test", "fake-model", "v0",
	)
	if err != nil {
		t.Fatal(err)
	}
	var observed []string
	_, err = CollectObserved(
		context.Background(), manifest,
		deterministicSQLRunner{path: PathRawAssets}, &deterministicExecution{},
		deterministicSQLRunner{path: PathMetis}, &deterministicExecution{},
		func(record AttemptRecord) error {
			observed = append(observed, string(record.Path)+"/"+record.Scenario)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]string, 0, 2*len(manifest.Scenarios))
	for _, scenario := range manifest.Scenarios {
		want = append(want,
			string(PathRawAssets)+"/"+scenario,
			string(PathMetis)+"/"+scenario,
		)
	}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("observer order = %v, want paired order %v", observed, want)
	}
}

func TestCollectArmObservedFromSkipsCompletedIdentity(t *testing.T) {
	manifest, err := NewDiagnosticAgentRunManifest(
		AgentIdentity{Name: "test-agent", Version: "v1", Model: "fake-model"},
		"deterministic-test", "fake-model", "v0",
	)
	if err != nil {
		t.Fatal(err)
	}
	firstScenario, err := ScenariosForManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := RunWithExecutionObservedScenarios(context.Background(), deterministicSQLRunner{path: PathMetis}, &deterministicExecution{}, manifest.Budget, firstScenario[:1], nil)
	if err != nil {
		t.Fatal(err)
	}
	observed := 0
	artifact, err := CollectArmObservedFrom(
		context.Background(), manifest, PathMetis,
		deterministicSQLRunner{path: PathMetis}, &deterministicExecution{}, seed,
		func(AttemptRecord) error { observed++; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := observed, len(manifest.Scenarios)-1; got != want {
		t.Fatalf("newly observed attempts = %d, want %d", got, want)
	}
	if len(artifact.Attempts) != len(manifest.Scenarios) {
		t.Fatalf("artifact attempts = %d, want %d", len(artifact.Attempts), len(manifest.Scenarios))
	}
}

func TestCollectRefusesSwappedArmIdentity(t *testing.T) {
	manifest, err := NewRunManifest("deterministic-test", "fake-model", "v0")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	_, err = Collect(
		context.Background(),
		manifest,
		deterministicSQLRunner{path: PathMetis},
		&deterministicExecution{},
		deterministicSQLRunner{path: PathRawAssets},
		&deterministicExecution{},
	)
	if err == nil {
		t.Fatal("Collect accepted swapped benchmark arms")
	}
}

func TestCollectSmokeRunsExactlyOneScenarioOnBothArms(t *testing.T) {
	manifest, err := NewSmokeAgentRunManifest(
		AgentIdentity{Name: "test-agent", Version: "v1", Model: "fake-model"},
		"deterministic-test", "fake-model", "v0", "conversion_rate_by_campaign",
	)
	if err != nil {
		t.Fatal(err)
	}
	collection, err := Collect(
		context.Background(), manifest,
		deterministicSQLRunner{path: PathRawAssets}, &deterministicExecution{},
		deterministicSQLRunner{path: PathMetis}, &deterministicExecution{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.RawAssets.Attempts) != 1 || len(collection.Metis.Attempts) != 1 {
		t.Fatalf("smoke attempts raw/metis=%d/%d, want 1/1", len(collection.RawAssets.Attempts), len(collection.Metis.Attempts))
	}
	if collection.RawAssets.Attempts[0].Scenario != manifest.Scenarios[0] || collection.Metis.Attempts[0].Scenario != manifest.Scenarios[0] {
		t.Fatalf("smoke collection ran wrong scenario: raw=%q metis=%q", collection.RawAssets.Attempts[0].Scenario, collection.Metis.Attempts[0].Scenario)
	}
	report, err := BuildV0Report(collection)
	if err != nil {
		t.Fatalf("BuildV0Report(smoke): %v", err)
	}
	if report.Manifest.RunMode != RunModeSmoke {
		t.Fatalf("smoke report mode=%q", report.Manifest.RunMode)
	}
}
