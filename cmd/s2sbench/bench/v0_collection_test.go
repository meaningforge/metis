package s2sbench

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

func TestCollectAndBuildV0ReportWiresTheFrozenDualArmRun(t *testing.T) {
	manifest, err := NewRunManifest("deterministic-test", "fake-model", "v0")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}

	rawClient := &deterministicRawClient{}
	metisClient := &deterministicMetisClient{}
	compiler := &deterministicSemanticCompiler{}
	rawExecution := &deterministicExecution{}
	metisExecution := &deterministicExecution{}

	collection, report, err := CollectAndBuildV0Report(context.Background(), V0CollectionConfig{
		Manifest:  manifest,
		RawClient: rawClient,
		RawAssets: FixtureRawAssetsProvider{PhysicalSchema: func(_ context.Context, scenario scenarios.Scenario) (string, error) {
			return "CREATE TABLE fixture_" + string(scenario.Fixture) + " (...)", nil
		}},
		RawExecution:   rawExecution,
		MetisClient:    metisClient,
		MetisCompiler:  compiler,
		MetisExecution: metisExecution,
	})
	if err != nil {
		t.Fatalf("CollectAndBuildV0Report: %v", err)
	}

	wantAttempts := len(manifest.Scenarios) * manifest.Budget.Repetitions
	if len(collection.RawAssets.Attempts) != wantAttempts || len(collection.Metis.Attempts) != wantAttempts {
		t.Fatalf("artifact attempts raw=%d metis=%d, want %d each", len(collection.RawAssets.Attempts), len(collection.Metis.Attempts), wantAttempts)
	}
	if rawClient.generateCalls != wantAttempts || metisClient.generateCalls != wantAttempts {
		t.Fatalf("model calls raw=%d metis=%d, want %d each", rawClient.generateCalls, metisClient.generateCalls, wantAttempts)
	}
	if compiler.calls != wantAttempts {
		t.Fatalf("compile calls = %d, want %d", compiler.calls, wantAttempts)
	}
	if report.Manifest.ModelProvider != manifest.ModelProvider || report.Manifest.ModelID != manifest.ModelID || report.Manifest.ModelVersion != manifest.ModelVersion {
		t.Fatalf("report lost pinned model identity: %+v", report.Manifest)
	}
	for _, stratum := range Strata {
		if got := report.RawAssets.Summary.ByStratum[stratum]; got.Wrong != 0 || got.Failed != 0 {
			t.Fatalf("raw stratum %s = %+v", stratum, got)
		}
		if got := report.Metis.Summary.ByStratum[stratum]; got.Wrong != 0 || got.Failed != 0 {
			t.Fatalf("Metis stratum %s = %+v", stratum, got)
		}
	}
}

func TestCollectV0RefusesFrozenFrameDriftBeforeClientCalls(t *testing.T) {
	manifest, err := NewRunManifest("deterministic-test", "fake-model", "v0")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	manifest.PromptVersion = "drifted-after-results"

	rawClient := &deterministicRawClient{}
	metisClient := &deterministicMetisClient{}
	_, err = CollectV0(context.Background(), V0CollectionConfig{
		Manifest:    manifest,
		RawClient:   rawClient,
		MetisClient: metisClient,
	})
	if err == nil || !strings.Contains(err.Error(), "prompt version") {
		t.Fatalf("err = %v; want frozen prompt-version refusal", err)
	}
	if rawClient.generateCalls != 0 || metisClient.generateCalls != 0 {
		t.Fatalf("drifted manifest reached model clients: raw=%d metis=%d", rawClient.generateCalls, metisClient.generateCalls)
	}
}
