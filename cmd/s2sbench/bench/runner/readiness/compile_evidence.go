package readiness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

type CompileEvidence struct {
	Observed       bool   `json:"observed"`
	ParseOK        bool   `json:"parse_ok"`
	ExecutionOK    bool   `json:"execution_ok"`
	OracleOK       bool   `json:"oracle_ok"`
	SQLFingerprint string `json:"sql_fingerprint,omitempty"`
	FailureDetail  string `json:"failure_detail,omitempty"`
}

func finalizeCompileEvidence(parent context.Context, execution Execution, scenario scenarios.Scenario, expectedDialect string, records []AttemptRecord) []AttemptRecord {
	for index := range records {
		record := &records[index]
		if record.CompileEvidence != nil {
			continue
		}
		scored, observed := scoreCapturedCompile(parent, execution, scenario, expectedDialect, *record)
		if observed {
			record.CompileEvidence = scored.CompileEvidence
			record.HandoffMatchesCompile = scored.HandoffMatchesCompile
		}
	}
	return records
}

func scoreCapturedCompile(parent context.Context, execution Execution, scenario scenarios.Scenario, expectedDialect string, record AttemptRecord) (AttemptRecord, bool) {
	response := lastSuccessfulCompileResponse(record.ToolTrace)
	if response == "" {
		return record, false
	}
	evidence := &CompileEvidence{Observed: true}
	query, err := decodeCompileToolResponse(response)
	if err != nil {
		evidence.FailureDetail = boundedFailureDetail(err)
		record.CompileEvidence = evidence
		record.Verdict = "failed"
		record.FailureCategory = "compile_response_invalid"
		record.FailureDetail = evidence.FailureDetail
		return record, true
	}
	if !strings.EqualFold(string(query.Dialect), expectedDialect) {
		evidence.FailureDetail = fmt.Sprintf("compile result dialect %q does not match manifest dialect", query.Dialect)
		record.CompileEvidence = evidence
		record.Verdict = "failed"
		record.FailureCategory = "compile_response_invalid"
		record.FailureDetail = evidence.FailureDetail
		return record, true
	}
	statement, err := ValidateReadOnlySQL(query.SQL)
	if err != nil {
		evidence.FailureDetail = boundedFailureDetail(err)
		record.CompileEvidence = evidence
		record.Verdict = "failed"
		record.FailureCategory = "compile_response_invalid"
		record.FailureDetail = evidence.FailureDetail
		return record, true
	}
	evidence.ParseOK = true
	evidence.SQLFingerprint = SQLFingerprint(statement, query.Parameters...)
	record.ParseOK = true
	record.AnswerStatus = "compiled"
	record.SQLFingerprint = evidence.SQLFingerprint
	record.HandoffMatchesCompile = true
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	actual, executionErr := execution.RunSQL(ctx, statement, query.Parameters...)
	cancel()
	if executionErr != nil {
		evidence.FailureDetail = boundedFailureDetail(executionErr)
		record.CompileEvidence = evidence
		record.Verdict = "failed"
		record.FailureCategory = "sql_execution_failed"
		record.FailureDetail = evidence.FailureDetail
		return record, true
	}
	evidence.ExecutionOK = true
	record.ExecutionOK = true
	verdict, judgeErr := s2sbench.Judge(scenario, actual)
	if verdict == s2sbench.VerdictCorrect && judgeErr == nil {
		evidence.OracleOK = true
		record.OracleOK = true
		record.Verdict = "correct"
	} else {
		evidence.FailureDetail = boundedFailureDetail(judgeErr)
		record.Verdict = "silent_wrong"
		record.FailureCategory = resultMismatchCategory(judgeErr)
		record.FailureDetail = evidence.FailureDetail
	}
	record.CompileEvidence = evidence
	return record, true
}

func lastSuccessfulCompileResponse(trace []s2sbench.ToolCallEvidence) string {
	return lastSuccessfulToolResponse(trace, "compile_sql")
}

func decodeCompileToolResponse(raw string) (sql.SQLRenderResult, error) {
	var value any
	if err := decodeJSONNumbers([]byte(raw), &value); err != nil {
		return sql.SQLRenderResult{}, fmt.Errorf("decode compile tool response: %w", err)
	}
	if query, ok := renderResultFromValue(value, 0); ok {
		return query, nil
	}
	return sql.SQLRenderResult{}, fmt.Errorf("compile tool response contains no render_result")
}

func renderResultFromValue(value any, depth int) (sql.SQLRenderResult, bool) {
	if depth > 8 {
		return sql.SQLRenderResult{}, false
	}
	switch current := value.(type) {
	case map[string]any:
		for key, nested := range current {
			if strings.EqualFold(key, "render_result") {
				if query, ok := decodeRenderResult(nested); ok {
					return query, true
				}
			}
		}
		if query, ok := decodeRenderResult(current); ok {
			return query, true
		}
		for _, nested := range current {
			if query, ok := renderResultFromValue(nested, depth+1); ok {
				return query, true
			}
		}
	case []any:
		for _, nested := range current {
			if query, ok := renderResultFromValue(nested, depth+1); ok {
				return query, true
			}
		}
	case string:
		var nested any
		if decodeJSONNumbers([]byte(current), &nested) == nil {
			return renderResultFromValue(nested, depth+1)
		}
	}
	return sql.SQLRenderResult{}, false
}

func decodeRenderResult(value any) (sql.SQLRenderResult, bool) {
	body, err := json.Marshal(value)
	if err != nil {
		return sql.SQLRenderResult{}, false
	}
	var query sql.SQLRenderResult
	if decodeJSONNumbers(body, &query) != nil || strings.TrimSpace(string(query.Dialect)) == "" || strings.TrimSpace(query.SQL) == "" {
		return sql.SQLRenderResult{}, false
	}
	return query, true
}

// Preserve numeric bindings when a compile result passes through JSON evidence.
func decodeJSONNumbers(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON: %v", err)
	}
	return nil
}
