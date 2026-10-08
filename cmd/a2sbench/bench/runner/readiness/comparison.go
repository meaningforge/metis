package readiness

import (
	"fmt"
	"reflect"
)

type ComparedArmOutcome struct {
	FirstReady                bool    `json:"first_attempt_ready"`
	SemanticIntentSuccess     bool    `json:"semantic_intent_success"`
	HandoffSuccess            bool    `json:"handoff_success"`
	EndToEndSuccess           bool    `json:"end_to_end_success"`
	FirstReadyRate            float64 `json:"first_attempt_readiness_rate"`
	SemanticIntentSuccessRate float64 `json:"semantic_intent_success_rate"`
	HandoffSuccessRate        float64 `json:"handoff_success_rate"`
	EndToEndSuccessRate       float64 `json:"end_to_end_success_rate"`
	FinalVerdict              string  `json:"final_verdict"`
}

type ComparisonOutcome struct {
	QuestionNumber int                        `json:"question_number"`
	Scenario       string                     `json:"scenario"`
	Question       string                     `json:"question"`
	Stratum        string                     `json:"stratum"`
	Arms           map[Arm]ComparedArmOutcome `json:"arms"`
}

type ComparedArmSummary struct {
	FirstReady                int     `json:"first_attempt_ready"`
	ReadinessRate             float64 `json:"first_attempt_readiness_rate"`
	SemanticIntentSuccessRate float64 `json:"semantic_intent_success_rate"`
	HandoffSuccessRate        float64 `json:"handoff_success_rate"`
	EndToEndSuccessRate       float64 `json:"end_to_end_success_rate"`
	Valid                     bool    `json:"valid"`
	InvalidReason             string  `json:"invalid_reason,omitempty"`
}

type PairwiseDelta struct {
	Left  Arm     `json:"left"`
	Right Arm     `json:"right"`
	Delta float64 `json:"first_attempt_readiness_delta"`
}

type ComparisonReport struct {
	SchemaVersion string                     `json:"schema_version"`
	Suite         Suite                      `json:"suite"`
	ArmOrder      []Arm                      `json:"arm_order"`
	Questions     []ComparisonOutcome        `json:"questions"`
	Summaries     map[Arm]ComparedArmSummary `json:"summaries"`
	Pairwise      []PairwiseDelta            `json:"pairwise"`
	Conclusion    string                     `json:"conclusion"`
	Valid         bool                       `json:"valid"`
	InvalidReason string                     `json:"invalid_reason,omitempty"`
}

// BuildComparisonReport aligns the independently collected OKF and Metis MCP
// arms by frozen one-based question number and verifies the full frame identity.
func BuildComparisonReport(collections ...Collection) (ComparisonReport, error) {
	if len(collections) != 2 {
		return ComparisonReport{}, fmt.Errorf("analysis requires exactly the OKF and Metis MCP single-arm collections")
	}
	base := collections[0]
	if err := base.Manifest.Validate(); err != nil {
		return ComparisonReport{}, fmt.Errorf("validate input 1 manifest: %w", err)
	}
	report := ComparisonReport{
		SchemaVersion: "s2sbench-comparison-report-v3", Suite: base.Manifest.Suite,
		Summaries:  make(map[Arm]ComparedArmSummary, len(collections)),
		Conclusion: string(base.Manifest.Suite) + "_comparison", Valid: true,
	}
	armReports := make([]Report, len(collections))
	seen := make(map[Arm]struct{}, len(collections))
	for index, collection := range collections {
		if err := collection.Manifest.Validate(); err != nil {
			return ComparisonReport{}, fmt.Errorf("validate input %d manifest: %w", index+1, err)
		}
		if _, duplicate := seen[collection.Manifest.Arm]; duplicate {
			return ComparisonReport{}, fmt.Errorf("arm %q was supplied more than once", collection.Manifest.Arm)
		}
		seen[collection.Manifest.Arm] = struct{}{}
		if !sameComparisonFrame(base.Manifest, collection.Manifest) {
			return ComparisonReport{}, fmt.Errorf("input %d does not describe the same frozen comparison frame", index+1)
		}
		if base.FactLedgerDigest == "" || collection.FactLedgerDigest != base.FactLedgerDigest {
			return ComparisonReport{}, fmt.Errorf("input %d fact-ledger digest does not match", index+1)
		}
		armReport, err := BuildReport(collection)
		if err != nil {
			return ComparisonReport{}, fmt.Errorf("build %s arm report: %w", collection.Manifest.Arm, err)
		}
		armReports[index] = armReport
		report.ArmOrder = append(report.ArmOrder, collection.Manifest.Arm)
		report.Summaries[collection.Manifest.Arm] = ComparedArmSummary{FirstReady: armReport.FirstReady, ReadinessRate: armReport.ReadinessRate, SemanticIntentSuccessRate: armReport.SemanticIntentSuccessRate, HandoffSuccessRate: armReport.HandoffSuccessRate, EndToEndSuccessRate: armReport.EndToEndSuccessRate, Valid: armReport.Valid, InvalidReason: armReport.InvalidReason}
		if !armReport.Valid {
			report.Valid = false
			if report.InvalidReason == "" {
				report.InvalidReason = string(collection.Manifest.Arm) + ": " + armReport.InvalidReason
			}
		}
	}
	if _, ok := seen[ArmOKF]; !ok {
		return ComparisonReport{}, fmt.Errorf("analysis requires the %q arm", ArmOKF)
	}
	if _, ok := seen[ArmMetisMCP]; !ok {
		return ComparisonReport{}, fmt.Errorf("analysis requires the %q arm", ArmMetisMCP)
	}
	for questionIndex, baseQuestion := range armReports[0].Questions {
		outcome := ComparisonOutcome{QuestionNumber: baseQuestion.QuestionNumber, Scenario: baseQuestion.Scenario, Question: baseQuestion.Question, Stratum: baseQuestion.Stratum, Arms: make(map[Arm]ComparedArmOutcome, len(collections))}
		for armIndex, armReport := range armReports {
			question := armReport.Questions[questionIndex]
			if question.QuestionNumber != baseQuestion.QuestionNumber || question.Scenario != baseQuestion.Scenario || question.Question != baseQuestion.Question || question.Stratum != baseQuestion.Stratum {
				return ComparisonReport{}, fmt.Errorf("question %d identity differs for arm %q", questionIndex+1, report.ArmOrder[armIndex])
			}
			outcome.Arms[report.ArmOrder[armIndex]] = ComparedArmOutcome{FirstReady: question.FirstReady, SemanticIntentSuccess: question.SemanticIntentSuccess, HandoffSuccess: question.HandoffSuccess, EndToEndSuccess: question.EndToEndSuccess, FirstReadyRate: question.FirstReadyRate, SemanticIntentSuccessRate: question.SemanticIntentSuccessRate, HandoffSuccessRate: question.HandoffSuccessRate, EndToEndSuccessRate: question.EndToEndSuccessRate, FinalVerdict: question.FinalVerdict}
		}
		report.Questions = append(report.Questions, outcome)
	}
	for left := 0; left < len(report.ArmOrder); left++ {
		for right := left + 1; right < len(report.ArmOrder); right++ {
			leftSummary := report.Summaries[report.ArmOrder[left]]
			rightSummary := report.Summaries[report.ArmOrder[right]]
			report.Pairwise = append(report.Pairwise, PairwiseDelta{Left: report.ArmOrder[left], Right: report.ArmOrder[right], Delta: leftSummary.ReadinessRate - rightSummary.ReadinessRate})
		}
	}
	return report, nil
}

func sameComparisonFrame(left, right Manifest) bool {
	left.Arm = ""
	right.Arm = ""
	return reflect.DeepEqual(left, right)
}
