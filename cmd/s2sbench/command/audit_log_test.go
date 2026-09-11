package command

import (
	"strings"
	"testing"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

func TestFormatAttemptAuditLogIsOneDelimitedLine(t *testing.T) {
	record := s2sbench.AttemptRecord{
		Path:       s2sbench.PathMetis,
		Scenario:   "aggregation_variants",
		Repetition: 1,
		Index:      2,
		Question:   "first line\nsecond | line",
		StartedAt:  time.Date(2026, 8, 25, 6, 34, 1, 103_000_000, time.UTC),
		DurationMS: 756,
		SQL:        "SELECT 'a|b'\nFROM orders WHERE purchased_at >= signup_at AND purchased_at <= signup_at + INTERVAL '7 days'",
		Verdict:    s2sbench.VerdictCorrect,
	}

	got, err := formatAttemptAuditLog(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("audit record spans multiple lines: %q", got)
	}
	for _, want := range []string{
		"Path=metis|Scenario=aggregation_variants|Repetition=1|Attempt=2",
		"Timestamp=2026-08-25T06:34:01.103Z|Time(ms)=756|Verdict=correct",
		`Question="first line\nsecond \u007c line"`,
		`SQL="SELECT 'a\u007cb' FROM orders WHERE purchased_at >= signup_at AND purchased_at <= signup_at + INTERVAL '7 days'"`,
		"ToolCalls=0|ContextTokens=0|OutputTokens=0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("audit record %q does not contain %q", got, want)
		}
	}
	for _, omitted := range []string{"SemanticQuery=", "ActualResult=", "ExpectedResult="} {
		if strings.Contains(got, omitted) {
			t.Errorf("audit record %q unexpectedly contains %q", got, omitted)
		}
	}
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u003e`) {
		t.Fatalf("audit SQL contains HTML-safe comparison escapes: %q", got)
	}
}

func TestFormatABSummaryAuditLog(t *testing.T) {
	got := formatABSummaryAuditLog(s2sbench.ABComparison{
		Metric: "observed_semantic_accuracy",
		A: s2sbench.ABArmSummary{
			Path:                    s2sbench.PathRawAssets,
			Total:                   10,
			Correct:                 6,
			Wrong:                   3,
			Failed:                  1,
			AccuracyPercent:         60,
			FirstTryAccuracyPercent: 50,
		},
		B: s2sbench.ABArmSummary{
			Path:                    s2sbench.PathMetis,
			Total:                   10,
			Correct:                 8,
			Wrong:                   1,
			Failed:                  1,
			AccuracyPercent:         80,
			FirstTryAccuracyPercent: 70,
		},
		Winner:                      "metis",
		MetisUpliftPercentagePoints: 20,
	})
	for _, want := range []string{"Summary=AB", "AAccuracy=60.00%", "BAccuracy=80.00%", "Winner=metis", "MetisUplift(pp)=20.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q does not contain %q", got, want)
		}
	}
}
