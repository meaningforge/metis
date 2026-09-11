package readiness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	service "github.com/meaningforge/metis/app/service/semantic"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/ossie"
)

type QueryEvidence struct {
	Observed      bool   `json:"observed"`
	QueryID       string `json:"query_id,omitempty"`
	ParseOK       bool   `json:"parse_ok"`
	ExecutionOK   bool   `json:"execution_ok"`
	OracleOK      bool   `json:"oracle_ok"`
	FailureDetail string `json:"failure_detail,omitempty"`
}

func scoreCapturedQueryMetrics(scenario scenarios.Scenario, record AttemptRecord) (AttemptRecord, bool) {
	response := lastSuccessfulToolResponse(record.ToolTrace, "query_metrics")
	if response == "" {
		return record, false
	}
	evidence := &QueryEvidence{Observed: true, ExecutionOK: true}
	result, err := decodeQueryMetricsToolResponse(response)
	if err != nil {
		evidence.FailureDetail = boundedFailureDetail(err)
		record.QueryEvidence = evidence
		record.Verdict = "failed"
		record.FailureCategory = "query_response_invalid"
		record.FailureDetail = evidence.FailureDetail
		return record, true
	}
	evidence.QueryID = result.QueryID
	actual, err := queryMetricsScenarioResult(result)
	if err != nil {
		evidence.FailureDetail = boundedFailureDetail(err)
		record.QueryEvidence = evidence
		record.Verdict = "failed"
		record.FailureCategory = "query_response_invalid"
		record.FailureDetail = evidence.FailureDetail
		return record, true
	}
	evidence.ParseOK = true
	record.ParseOK = true
	record.ExecutionOK = true
	record.AnswerStatus = "queried"
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
	record.QueryEvidence = evidence
	return record, true
}

func lastSuccessfulToolResponse(trace []s2sbench.ToolCallEvidence, tool string) string {
	response := ""
	for _, call := range trace {
		name := strings.TrimSpace(call.Name)
		if call.Status == "success" && (name == tool || strings.HasSuffix(name, "."+tool) || strings.HasSuffix(name, "_"+tool)) && strings.TrimSpace(call.Response) != "" {
			response = call.Response
		}
	}
	return response
}

func decodeQueryMetricsToolResponse(raw string) (service.QueryMetricsResult, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return service.QueryMetricsResult{}, fmt.Errorf("decode query_metrics tool response: %w", err)
	}
	if result, ok := queryMetricsResultFromValue(value, 0); ok {
		return result, nil
	}
	return service.QueryMetricsResult{}, fmt.Errorf("query_metrics tool response contains no structured result")
}

func queryMetricsResultFromValue(value any, depth int) (service.QueryMetricsResult, bool) {
	if depth > 8 {
		return service.QueryMetricsResult{}, false
	}
	if object, ok := value.(map[string]any); ok {
		if _, hasRows := object["rows"]; hasRows {
			body, err := json.Marshal(object)
			var result service.QueryMetricsResult
			if err == nil && json.Unmarshal(body, &result) == nil && len(result.Schema.Columns) > 0 {
				return result, true
			}
		}
		for _, nested := range object {
			if result, ok := queryMetricsResultFromValue(nested, depth+1); ok {
				return result, true
			}
		}
	}
	if values, ok := value.([]any); ok {
		for _, nested := range values {
			if result, ok := queryMetricsResultFromValue(nested, depth+1); ok {
				return result, true
			}
		}
	}
	if text, ok := value.(string); ok {
		return decodeNestedQueryMetricsResult(text, depth)
	}
	return service.QueryMetricsResult{}, false
}

func decodeNestedQueryMetricsResult(text string, depth int) (service.QueryMetricsResult, bool) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var nested any
	if decoder.Decode(&nested) != nil {
		return service.QueryMetricsResult{}, false
	}
	return queryMetricsResultFromValue(nested, depth+1)
}

func queryMetricsScenarioResult(result service.QueryMetricsResult) (scenarios.ResultSet, error) {
	if result.Count != int64(len(result.Rows)) {
		return scenarios.ResultSet{}, fmt.Errorf("query_metrics count %d does not match %d rows", result.Count, len(result.Rows))
	}
	columns := make([]scenarios.ResultColumn, len(result.Schema.Columns))
	for index, column := range result.Schema.Columns {
		kind, err := semanticResultKind(column.Datatype)
		if err != nil {
			return scenarios.ResultSet{}, fmt.Errorf("output column %q: %w", column.Name, err)
		}
		columns[index] = scenarios.ResultColumn{Name: column.Name, ValueKind: kind}
	}
	rows := make([]scenarios.ResultRow, len(result.Rows))
	for rowIndex, values := range result.Rows {
		if len(values) != len(columns) {
			return scenarios.ResultSet{}, fmt.Errorf("query_metrics row %d has %d values, want %d", rowIndex, len(values), len(columns))
		}
		row := make(scenarios.ResultRow, len(values))
		for columnIndex, raw := range values {
			kind := columns[columnIndex].ValueKind
			if raw == nil {
				row[columnIndex] = scenarios.NullResultValue(kind)
				continue
			}
			literal, err := semanticResultLiteral(raw)
			if err != nil {
				return scenarios.ResultSet{}, fmt.Errorf("query_metrics row %d column %q: %w", rowIndex, columns[columnIndex].Name, err)
			}
			row[columnIndex], err = scenarios.ParseResultValue(kind, literal)
			if err != nil {
				return scenarios.ResultSet{}, fmt.Errorf("query_metrics row %d column %q: %w", rowIndex, columns[columnIndex].Name, err)
			}
		}
		rows[rowIndex] = row
	}
	return scenarios.ResultSet{Columns: columns, Rows: rows}, nil
}

func semanticResultKind(datatype ossie.DataType) (scenarios.ResultValueKind, error) {
	switch datatype {
	case ossie.DataTypeString:
		return scenarios.ResultString, nil
	case ossie.DataTypeInteger:
		return scenarios.ResultInteger, nil
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		return scenarios.ResultNumber, nil
	case ossie.DataTypeBoolean:
		return scenarios.ResultBoolean, nil
	case ossie.DataTypeDate:
		return scenarios.ResultDate, nil
	case ossie.DataTypeTime:
		return scenarios.ResultTime, nil
	case ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return scenarios.ResultDateTime, nil
	case "", ossie.DataTypeOpaque:
		return scenarios.ResultOpaque, nil
	default:
		return "", fmt.Errorf("unsupported semantic datatype %q", datatype)
	}
}

func semanticResultLiteral(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case json.Number:
		return typed.String(), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported normalized result value %T", value)
	}
}
