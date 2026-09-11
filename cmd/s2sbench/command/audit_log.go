package command

import (
	"encoding/json"
	"fmt"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	benchartifact "github.com/meaningforge/metis/cmd/s2sbench/bench/artifact"
)

// formatAttemptAuditLog renders one completed repetition as a single,
// grep-friendly audit record. Free-form strings and structured values remain
// valid on one line even when they contain newlines or field delimiters.
func formatAttemptAuditLog(record s2sbench.AttemptRecord) (string, error) {
	fields := []string{
		"Path=" + escapeAuditDelimiter(string(record.Path)),
		"Scenario=" + escapeAuditDelimiter(record.Scenario),
		fmt.Sprintf("Repetition=%d", record.Repetition),
		fmt.Sprintf("Attempt=%d", record.Index),
		"Timestamp=" + record.StartedAt.Format("2006-01-02T15:04:05.000Z07:00"),
		fmt.Sprintf("Time(ms)=%d", record.DurationMS),
		"Verdict=" + escapeAuditDelimiter(string(record.Verdict)),
		"Question=" + auditText(record.Question),
		"SQL=" + auditText(singleLineSQL(record.SQL)),
		fmt.Sprintf("ToolCalls=%d", record.ToolCalls),
		fmt.Sprintf("ContextTokens=%d", record.ContextTokens),
		fmt.Sprintf("OutputTokens=%d", record.OutputTokens),
		"Reason=" + auditText(record.Reason),
		"Error=" + auditText(record.Error),
	}
	return strings.Join(fields, "|"), nil
}

func singleLineSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "\r\n", " ")
	sql = strings.ReplaceAll(sql, "\r", " ")
	return strings.ReplaceAll(sql, "\n", " ")
}

func formatABSummaryAuditLog(comparison s2sbench.ABComparison) string {
	return strings.Join([]string{
		"Summary=AB",
		"Metric=" + comparison.Metric,
		"APath=" + string(comparison.A.Path),
		fmt.Sprintf("ATotal=%d", comparison.A.Total),
		fmt.Sprintf("ACorrect=%d", comparison.A.Correct),
		fmt.Sprintf("AWrong=%d", comparison.A.Wrong),
		fmt.Sprintf("AFailed=%d", comparison.A.Failed),
		fmt.Sprintf("AAccuracy=%.2f%%", comparison.A.AccuracyPercent),
		fmt.Sprintf("AFirstTryAccuracy=%.2f%%", comparison.A.FirstTryAccuracyPercent),
		"BPath=" + string(comparison.B.Path),
		fmt.Sprintf("BTotal=%d", comparison.B.Total),
		fmt.Sprintf("BCorrect=%d", comparison.B.Correct),
		fmt.Sprintf("BWrong=%d", comparison.B.Wrong),
		fmt.Sprintf("BFailed=%d", comparison.B.Failed),
		fmt.Sprintf("BAccuracy=%.2f%%", comparison.B.AccuracyPercent),
		fmt.Sprintf("BFirstTryAccuracy=%.2f%%", comparison.B.FirstTryAccuracyPercent),
		"Winner=" + comparison.Winner,
		fmt.Sprintf("MetisUplift(pp)=%.2f", comparison.MetisUpliftPercentagePoints),
	}, "|")
}

func auditText(value string) string {
	encoded, _ := encodeAuditJSON(benchartifact.RedactString(value))
	return escapeAuditDelimiter(encoded)
}

func encodeAuditJSON(value any) (string, error) {
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(encoded.String(), "\n"), nil
}

func escapeAuditDelimiter(value string) string {
	return strings.ReplaceAll(value, "|", `\u007c`)
}
