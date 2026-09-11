package s2sbench

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
)

// UsageReport is derived accounting evidence for one arm. Raw per-turn usage
// in each question trace remains authoritative; these totals are only a view.
type UsageReport struct {
	ToolCalls         int `json:"tool_calls"`
	SemanticToolCalls int `json:"semantic_tool_calls"`
	ToolCallsP50      int `json:"tool_calls_p50"`
	ToolCallsP90      int `json:"tool_calls_p90"`
	SemanticCallsP50  int `json:"semantic_calls_p50"`
	SemanticCallsP90  int `json:"semantic_calls_p90"`
	LatencyMSP50      int `json:"latency_ms_p50"`
	LatencyMSP90      int `json:"latency_ms_p90"`
	ContextTokens     int `json:"context_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// V0ArmReport combines the frozen semantic summary with aggregate usage. It
// deliberately does not compute cost, significance, or a cross-model claim.
// It does report observed latency and tool-call percentiles without claiming
// statistical significance.
type V0ArmReport struct {
	Summary Report      `json:"summary"`
	Usage   UsageReport `json:"usage"`
}

// ABArmSummary is the compact, directly comparable headline for one arm.
// Accuracy includes failed repetitions in the denominator; excluding them
// would make an unavailable or refusing Agent look better than it was.
type ABArmSummary struct {
	Path                    Path    `json:"path"`
	Total                   int     `json:"total"`
	Correct                 int     `json:"correct"`
	Wrong                   int     `json:"wrong"`
	Failed                  int     `json:"failed"`
	FirstTryCorrect         int     `json:"first_try_correct"`
	AccuracyPercent         float64 `json:"accuracy_percent"`
	FirstTryAccuracyPercent float64 `json:"first_try_accuracy_percent"`
}

// ABComparison gives A=raw_assets and B=metis an observed winner using only
// semantic accuracy. It is a descriptive result, not a significance claim.
type ABComparison struct {
	Metric                      string       `json:"metric"`
	A                           ABArmSummary `json:"a"`
	B                           ABArmSummary `json:"b"`
	Winner                      string       `json:"winner"`
	MetisUpliftPercentagePoints float64      `json:"metis_uplift_percentage_points"`
}

// V0Report is a fully derived view of one complete dual-arm collection. The
// shared manifest is retained so a report can never be separated from the exact
// model identity/version and frozen frame that produced its raw evidence.
type V0Report struct {
	Manifest   RunManifest      `json:"manifest"`
	RawAssets  V0ArmReport      `json:"raw_assets"`
	Metis      V0ArmReport      `json:"metis"`
	Comparison ABComparison     `json:"comparison"`
	Cases      []CaseComparison `json:"cases"`
}

// CaseArmEvidence is one arm's outcome and total observable resource usage for
// a stable scenario/repetition case. Retry usage is included in the totals.
type CaseArmEvidence struct {
	Verdict         Verdict `json:"verdict"`
	FirstTryCorrect bool    `json:"first_try_correct"`
	RepairedCorrect bool    `json:"repaired_correct"`
	Attempts        int     `json:"attempts"`
	DurationMS      int64   `json:"duration_ms"`
	ToolCalls       int     `json:"tool_calls"`
	ContextTokens   int     `json:"context_tokens"`
	OutputTokens    int     `json:"output_tokens"`
}

// CaseComparison aligns independently collectable arms by a stable number
// derived from the frozen manifest order, never by collection timing.
type CaseComparison struct {
	CaseID             string          `json:"case_id"`
	Scenario           string          `json:"scenario"`
	Stratum            Stratum         `json:"stratum"`
	Repetition         int             `json:"repetition"`
	RawAssets          CaseArmEvidence `json:"raw_assets"`
	Metis              CaseArmEvidence `json:"metis"`
	DurationDeltaMS    int64           `json:"metis_minus_raw_duration_ms"`
	ToolCallsDelta     int             `json:"metis_minus_raw_tool_calls"`
	ContextTokensDelta int             `json:"metis_minus_raw_context_tokens"`
	OutputTokensDelta  int             `json:"metis_minus_raw_output_tokens"`
}

// BuildV0Report derives the v0 report from immutable raw artifacts. Reporting
// must never repair, filter, resample, or otherwise tune the collected evidence
// after results are visible.
func BuildV0Report(collection Collection) (V0Report, error) {
	if !reflect.DeepEqual(collection.RawAssets.Manifest, collection.Metis.Manifest) {
		return V0Report{}, fmt.Errorf("benchmark arms do not share the identical frozen manifest")
	}
	manifest := collection.RawAssets.Manifest
	if err := manifest.ValidateForCollection(); err != nil {
		return V0Report{}, fmt.Errorf("validate report manifest: %w", err)
	}

	raw, err := deriveArmReport(collection.RawAssets, PathRawAssets)
	if err != nil {
		return V0Report{}, err
	}
	metis, err := deriveArmReport(collection.Metis, PathMetis)
	if err != nil {
		return V0Report{}, err
	}
	cases, err := buildCaseComparisons(collection.RawAssets, collection.Metis)
	if err != nil {
		return V0Report{}, err
	}
	return V0Report{Manifest: manifest, RawAssets: raw, Metis: metis, Comparison: buildABComparison(raw, metis), Cases: cases}, nil
}

func buildCaseComparisons(raw, metis ArmRunArtifact) ([]CaseComparison, error) {
	if len(raw.Attempts) != len(metis.Attempts) {
		return nil, fmt.Errorf("benchmark arms have different case counts")
	}
	selection, err := Selection()
	if err != nil {
		return nil, err
	}
	stratumOf := make(map[string]Stratum, len(raw.Manifest.Scenarios))
	for _, stratum := range Strata {
		for _, scenario := range selection[stratum] {
			stratumOf[scenario.Name] = stratum
		}
	}
	scenarioIndex := make(map[string]int, len(raw.Manifest.Scenarios))
	for index, scenario := range raw.Manifest.Scenarios {
		scenarioIndex[scenario] = index + 1
	}
	result := make([]CaseComparison, len(raw.Attempts))
	for index := range raw.Attempts {
		rawRecord, metisRecord := raw.Attempts[index], metis.Attempts[index]
		if rawRecord.Scenario != metisRecord.Scenario || rawRecord.Repetition != metisRecord.Repetition {
			return nil, fmt.Errorf("benchmark arms do not align at case %d", index+1)
		}
		rawEvidence := caseArmEvidence(rawRecord)
		metisEvidence := caseArmEvidence(metisRecord)
		result[index] = CaseComparison{
			CaseID:             fmt.Sprintf("S%02d-R%02d", scenarioIndex[rawRecord.Scenario], rawRecord.Repetition),
			Scenario:           rawRecord.Scenario,
			Stratum:            stratumOf[rawRecord.Scenario],
			Repetition:         rawRecord.Repetition,
			RawAssets:          rawEvidence,
			Metis:              metisEvidence,
			DurationDeltaMS:    metisEvidence.DurationMS - rawEvidence.DurationMS,
			ToolCallsDelta:     metisEvidence.ToolCalls - rawEvidence.ToolCalls,
			ContextTokensDelta: metisEvidence.ContextTokens - rawEvidence.ContextTokens,
			OutputTokensDelta:  metisEvidence.OutputTokens - rawEvidence.OutputTokens,
		}
	}
	return result, nil
}

func caseArmEvidence(record AttemptRecord) CaseArmEvidence {
	evidence := CaseArmEvidence{
		Verdict:         record.Verdict,
		FirstTryCorrect: record.Verdict == VerdictCorrect && record.Index == 1,
		RepairedCorrect: record.Verdict == VerdictCorrect && record.Index > 1,
		Attempts:        record.Index,
		DurationMS:      record.DurationMS,
	}
	if len(record.Trace) == 0 {
		evidence.ToolCalls = record.ToolCalls
		evidence.ContextTokens = record.ContextTokens
		evidence.OutputTokens = record.OutputTokens
		return evidence
	}
	for _, attempt := range record.Trace {
		evidence.ToolCalls += attempt.ToolCalls
		evidence.ContextTokens += attempt.ContextTokens
		evidence.OutputTokens += attempt.OutputTokens
	}
	return evidence
}

// BuildV0ArmReport derives the same per-arm summary used by the dual-arm
// report while preserving the artifact as the source of truth.
func BuildV0ArmReport(artifact ArmRunArtifact) (V0ArmReport, error) {
	return deriveArmReport(artifact, artifact.Path)
}

func buildABComparison(raw, metis V0ArmReport) ABComparison {
	a := headlineFor(PathRawAssets, raw.Summary)
	b := headlineFor(PathMetis, metis.Summary)
	winner := "tie"
	switch {
	case a.Correct*b.Total > b.Correct*a.Total:
		winner = string(PathRawAssets)
	case b.Correct*a.Total > a.Correct*b.Total:
		winner = string(PathMetis)
	}
	return ABComparison{
		Metric:                      "observed_semantic_accuracy",
		A:                           a,
		B:                           b,
		Winner:                      winner,
		MetisUpliftPercentagePoints: roundPercent(b.AccuracyPercent - a.AccuracyPercent),
	}
}

func headlineFor(path Path, summary Report) ABArmSummary {
	headline := ABArmSummary{Path: path}
	for _, stratum := range Strata {
		entry := summary.ByStratum[stratum]
		headline.Total += entry.Questions
		headline.Correct += entry.Correct
		headline.Wrong += entry.Wrong
		headline.Failed += entry.Failed
		headline.FirstTryCorrect += entry.FirstTryRight
	}
	if headline.Total > 0 {
		headline.AccuracyPercent = roundPercent(float64(headline.Correct) * 100 / float64(headline.Total))
		headline.FirstTryAccuracyPercent = roundPercent(float64(headline.FirstTryCorrect) * 100 / float64(headline.Total))
	}
	return headline
}

func roundPercent(value float64) float64 {
	return math.Round(value*100) / 100
}

func deriveArmReport(artifact ArmRunArtifact, wantPath Path) (V0ArmReport, error) {
	if artifact.Path != wantPath {
		return V0ArmReport{}, fmt.Errorf("artifact path is %q, want %q", artifact.Path, wantPath)
	}
	wantAttempts := len(artifact.Manifest.Scenarios) * artifact.Manifest.Budget.Repetitions
	if len(artifact.Attempts) != wantAttempts {
		return V0ArmReport{}, fmt.Errorf("%s artifact has %d attempts, want %d", wantPath, len(artifact.Attempts), wantAttempts)
	}

	attempts := make([]Attempt, 0, len(artifact.Attempts))
	var usage UsageReport
	var toolCalls, semanticCalls, latencies []int
	for _, record := range artifact.Attempts {
		if record.Path != wantPath {
			return V0ArmReport{}, fmt.Errorf("%s artifact contains record for path %q", wantPath, record.Path)
		}
		attempts = append(attempts, Attempt{
			Path:          record.Path,
			Scenario:      record.Scenario,
			Repetition:    record.Repetition,
			Index:         record.Index,
			SQL:           record.SQL,
			SemanticQuery: record.SemanticQuery,
			ToolCalls:     record.ToolCalls,
			ContextTokens: record.ContextTokens,
			OutputTokens:  record.OutputTokens,
			Result:        record.Result,
			Verdict:       record.Verdict,
			Reason:        record.Reason,
		})
		recordToolCalls, recordSemanticCalls := record.ToolCalls, len(record.ToolTrace)
		recordContextTokens, recordOutputTokens := record.ContextTokens, record.OutputTokens
		if len(record.Trace) > 0 {
			recordToolCalls, recordSemanticCalls, recordContextTokens, recordOutputTokens = 0, 0, 0, 0
			for _, evidence := range record.Trace {
				recordToolCalls += evidence.ToolCalls
				recordSemanticCalls += len(evidence.ToolTrace)
				recordContextTokens += evidence.ContextTokens
				recordOutputTokens += evidence.OutputTokens
			}
		}
		usage.ToolCalls += recordToolCalls
		usage.SemanticToolCalls += recordSemanticCalls
		usage.ContextTokens += recordContextTokens
		usage.OutputTokens += recordOutputTokens
		toolCalls = append(toolCalls, recordToolCalls)
		semanticCalls = append(semanticCalls, recordSemanticCalls)
		latencies = append(latencies, int(record.DurationMS))
	}
	usage.ToolCallsP50, usage.ToolCallsP90 = nearestRank(toolCalls, 0.50), nearestRank(toolCalls, 0.90)
	usage.SemanticCallsP50, usage.SemanticCallsP90 = nearestRank(semanticCalls, 0.50), nearestRank(semanticCalls, 0.90)
	usage.LatencyMSP50, usage.LatencyMSP90 = nearestRank(latencies, 0.50), nearestRank(latencies, 0.90)

	summary, err := Summarise(wantPath, attempts)
	if err != nil {
		return V0ArmReport{}, fmt.Errorf("summarise %s artifact: %w", wantPath, err)
	}
	return V0ArmReport{Summary: summary, Usage: usage}, nil
}

func nearestRank(values []int, percentile float64) int {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int(nil), values...)
	sort.Ints(ordered)
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	return ordered[index]
}

// WriteV0ReportJSON serializes only derived evidence. The raw arm artifacts
// remain the auditable source of truth and must be persisted independently.
func WriteV0ReportJSON(w io.Writer, report V0Report) error {
	if w == nil {
		return fmt.Errorf("report writer is required")
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode S2SBench v0 report: %w", err)
	}
	return nil
}
