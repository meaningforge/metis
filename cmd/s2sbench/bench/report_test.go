package s2sbench

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildV0ReportDerivesOnlyFromCompleteRawArtifacts(t *testing.T) {
	manifest, err := NewRunManifest("deterministic-test", "fake-model", "v0")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}

	rawAttempts := completeAttempts(t, PathRawAssets, manifest.Budget.Repetitions)
	metisAttempts := completeAttempts(t, PathMetis, manifest.Budget.Repetitions)
	for i := range rawAttempts {
		rawAttempts[i].ToolCalls = 1
		rawAttempts[i].ContextTokens = 10
		rawAttempts[i].OutputTokens = 2
		rawAttempts[i].Trace[0].ToolCalls = 1
		rawAttempts[i].Trace[0].ContextTokens = 10
		rawAttempts[i].Trace[0].OutputTokens = 2

		metisAttempts[i].ToolCalls = 2
		metisAttempts[i].ContextTokens = 7
		metisAttempts[i].OutputTokens = 1
		metisAttempts[i].Trace[0].ToolCalls = 2
		metisAttempts[i].Trace[0].ContextTokens = 7
		metisAttempts[i].Trace[0].OutputTokens = 1
		metisAttempts[i].ToolTrace = []ToolCallEvidence{{Name: "metis.list_metrics", DurationMS: 5, RequestBytes: 20, ResponseBytes: 40, Status: "success"}}
		metisAttempts[i].Trace[0].ToolTrace = append([]ToolCallEvidence(nil), metisAttempts[i].ToolTrace...)
	}

	raw, err := NewArmRunArtifact(manifest, PathRawAssets, rawAttempts)
	if err != nil {
		t.Fatalf("raw artifact: %v", err)
	}
	metis, err := NewArmRunArtifact(manifest, PathMetis, metisAttempts)
	if err != nil {
		t.Fatalf("Metis artifact: %v", err)
	}

	report, err := BuildV0Report(Collection{RawAssets: raw, Metis: metis})
	if err != nil {
		t.Fatalf("BuildV0Report: %v", err)
	}
	wantAttempts := len(manifest.Scenarios) * manifest.Budget.Repetitions
	if report.RawAssets.Usage.ContextTokens != wantAttempts*10 || report.Metis.Usage.ContextTokens != wantAttempts*7 {
		t.Fatalf("unexpected context totals: raw=%d metis=%d", report.RawAssets.Usage.ContextTokens, report.Metis.Usage.ContextTokens)
	}
	if report.RawAssets.Usage.ToolCalls != wantAttempts || report.Metis.Usage.ToolCalls != wantAttempts*2 {
		t.Fatalf("unexpected tool-call totals: raw=%d metis=%d", report.RawAssets.Usage.ToolCalls, report.Metis.Usage.ToolCalls)
	}
	if report.Metis.Usage.SemanticToolCalls != wantAttempts || report.Metis.Usage.SemanticCallsP50 != 1 || report.Metis.Usage.SemanticCallsP90 != 1 {
		t.Fatalf("unexpected semantic-call summary: %+v", report.Metis.Usage)
	}
	for _, stratum := range Strata {
		if got := report.RawAssets.Summary.ByStratum[stratum]; got.Questions == 0 || got.Wrong != 0 || got.Failed != 0 {
			t.Fatalf("raw stratum %s summary = %+v", stratum, got)
		}
		if got := report.Metis.Summary.ByStratum[stratum]; got.Questions == 0 || got.Wrong != 0 || got.Failed != 0 {
			t.Fatalf("Metis stratum %s summary = %+v", stratum, got)
		}
	}
	if len(report.Cases) != wantAttempts {
		t.Fatalf("case comparisons = %d, want %d", len(report.Cases), wantAttempts)
	}
	first := report.Cases[0]
	if first.CaseID != "S01-R01" || first.Scenario != manifest.Scenarios[0] || first.RawAssets.ContextTokens != 10 || first.Metis.ContextTokens != 7 || first.ContextTokensDelta != -3 {
		t.Fatalf("first case comparison = %+v", first)
	}

	var encoded bytes.Buffer
	if err := WriteV0ReportJSON(&encoded, report); err != nil {
		t.Fatalf("WriteV0ReportJSON: %v", err)
	}
	for _, want := range []string{`"model_id": "fake-model"`, `"raw_assets"`, `"metis"`, `"context_tokens"`, `"case_id": "S01-R01"`} {
		if !strings.Contains(encoded.String(), want) {
			t.Fatalf("encoded report does not contain %q", want)
		}
	}
}

func TestBuildV0ReportRefusesMismatchedFrozenManifests(t *testing.T) {
	manifest, err := NewRunManifest("deterministic-test", "fake-model", "v0")
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	raw, err := NewArmRunArtifact(manifest, PathRawAssets, completeAttempts(t, PathRawAssets, manifest.Budget.Repetitions))
	if err != nil {
		t.Fatal(err)
	}
	metis, err := NewArmRunArtifact(manifest, PathMetis, completeAttempts(t, PathMetis, manifest.Budget.Repetitions))
	if err != nil {
		t.Fatal(err)
	}
	metis.Manifest.ModelID = "different-model"

	if _, err := BuildV0Report(Collection{RawAssets: raw, Metis: metis}); err == nil || !strings.Contains(err.Error(), "identical frozen manifest") {
		t.Fatalf("err = %v; want frozen-manifest mismatch", err)
	}
}

func TestBuildABComparisonReportsObservedWinnerAndMetisUplift(t *testing.T) {
	raw := V0ArmReport{Summary: Report{ByStratum: map[Stratum]StratumReport{
		StratumSilentSemantics: {Questions: 10, Correct: 6, Wrong: 3, Failed: 1, FirstTryRight: 5},
	}}}
	metis := V0ArmReport{Summary: Report{ByStratum: map[Stratum]StratumReport{
		StratumSilentSemantics: {Questions: 10, Correct: 8, Wrong: 1, Failed: 1, FirstTryRight: 7},
	}}}

	comparison := buildABComparison(raw, metis)
	if comparison.Winner != string(PathMetis) {
		t.Fatalf("winner=%q, want metis", comparison.Winner)
	}
	if comparison.A.AccuracyPercent != 60 || comparison.B.AccuracyPercent != 80 || comparison.MetisUpliftPercentagePoints != 20 {
		t.Fatalf("comparison=%+v, want A=60%% B=80%% uplift=20pp", comparison)
	}
	if comparison.A.FirstTryAccuracyPercent != 50 || comparison.B.FirstTryAccuracyPercent != 70 {
		t.Fatalf("first-try comparison=%+v, want A=50%% B=70%%", comparison)
	}
	if comparison.A.Total != comparison.A.Correct+comparison.A.Wrong+comparison.A.Failed {
		t.Fatalf("A outcome counts do not cover denominator: %+v", comparison.A)
	}
}
