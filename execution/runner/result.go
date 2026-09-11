package runner

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

// ResultValueFamily is a closed, non-reflective description of a value's
// driver-decoded family. It never includes a concrete Go type or raw value.
type ResultValueFamily string

const (
	ResultFamilyText        ResultValueFamily = "text"
	ResultFamilyInteger     ResultValueFamily = "integer"
	ResultFamilyFloat       ResultValueFamily = "binary_float"
	ResultFamilyBoolean     ResultValueFamily = "boolean"
	ResultFamilyTemporal    ResultValueFamily = "temporal"
	ResultFamilyContainer   ResultValueFamily = "container"
	ResultFamilyUnsupported ResultValueFamily = "unsupported"
)

// ResultContractError identifies one value-free OutputSchema mismatch.
type ResultContractError struct {
	Column           string
	ExpectedDatatype ossie.DataType
	ObservedFamily   ResultValueFamily
}

func (e *ResultContractError) Error() string { return "result value does not satisfy output schema" }

// normalizeResultRow converts driver-decoded values into the stable
// Agent-facing JSON representation declared by OutputSchema.
func normalizeResultRow(row []any, schema artifact.OutputSchema) ([]any, error) {
	normalized := make([]any, len(row))
	for index, value := range row {
		if value == nil {
			continue
		}
		converted, err := normalizeResultValue(value, schema.Columns[index].Datatype)
		if err != nil {
			return nil, &ResultContractError{
				Column:           schema.Columns[index].Name,
				ExpectedDatatype: schema.Columns[index].Datatype,
				ObservedFamily:   resultValueFamily(value),
			}
		}
		normalized[index] = converted
	}
	return normalized, nil
}

func resultValueFamily(value any) ResultValueFamily {
	switch value.(type) {
	case string, []byte, json.Number:
		return ResultFamilyText
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return ResultFamilyInteger
	case float32, float64:
		return ResultFamilyFloat
	case bool:
		return ResultFamilyBoolean
	case time.Time:
		return ResultFamilyTemporal
	case []any, []string, []int, map[string]any, map[string]string:
		return ResultFamilyContainer
	default:
		return ResultFamilyUnsupported
	}
}

func normalizeResultValue(value any, datatype ossie.DataType) (any, error) {
	switch datatype {
	case "", ossie.DataTypeOpaque:
		return snapshotResultValue(value)
	case ossie.DataTypeString:
		return normalizedText(value)
	case ossie.DataTypeInteger:
		text, err := normalizedNumericText(value)
		if err != nil {
			return nil, err
		}
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, fmt.Errorf("invalid integer result")
		}
		return json.Number(integer.String()), nil
	case ossie.DataTypeDecimal:
		switch value.(type) {
		case float32, float64:
			return nil, fmt.Errorf("decimal result was decoded through a binary float")
		}
		text, err := normalizedNumericText(value)
		if err != nil {
			return nil, err
		}
		if _, ok := new(big.Rat).SetString(text); !ok || strings.Contains(text, "/") {
			return nil, fmt.Errorf("invalid decimal result")
		}
		// Decimal remains text so JSON encoding cannot lose database precision or
		// scale (for example, ClickHouse Decimal64 or Doris DECIMALV3).
		return text, nil
	case ossie.DataTypeFloat:
		text, err := normalizedNumericText(value)
		if err != nil {
			return nil, err
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return nil, fmt.Errorf("invalid float result")
		}
		return json.Number(text), nil
	case ossie.DataTypeBoolean:
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case string:
			return parseResultBoolean(typed)
		case []byte:
			return parseResultBoolean(string(typed))
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			text, err := normalizedNumericText(typed)
			if err != nil {
				return nil, err
			}
			return parseResultBoolean(text)
		default:
			return nil, fmt.Errorf("invalid boolean result")
		}
	case ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return normalizeTemporalValue(value, datatype)
	default:
		return nil, fmt.Errorf("unsupported output datatype")
	}
}

func normalizeTemporalValue(value any, datatype ossie.DataType) (string, error) {
	if instant, ok := value.(time.Time); ok {
		if datatype == ossie.DataTypeDate && (instant.Hour() != 0 || instant.Minute() != 0 || instant.Second() != 0 || instant.Nanosecond() != 0) {
			return "", fmt.Errorf("invalid %s result", datatype)
		}
		return formatTemporalValue(instant, datatype), nil
	}
	text, err := normalizedText(value)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	var instant time.Time
	switch datatype {
	case ossie.DataTypeDate:
		instant, err = time.Parse("2006-01-02", text)
	case ossie.DataTypeTime:
		instant, err = time.Parse("15:04:05.999999999", text)
	case ossie.DataTypeDateTime:
		instant, err = parseTemporalLayouts(text,
			"2006-01-02T15:04:05.999999999",
			"2006-01-02 15:04:05.999999999",
		)
	case ossie.DataTypeDateTimeTz:
		instant, err = time.Parse(time.RFC3339Nano, text)
	default:
		return "", fmt.Errorf("unsupported temporal datatype")
	}
	if err != nil {
		return "", fmt.Errorf("invalid %s result", datatype)
	}
	return formatTemporalValue(instant, datatype), nil
}

func parseTemporalLayouts(value string, layouts ...string) (time.Time, error) {
	var err error
	for _, layout := range layouts {
		var instant time.Time
		instant, err = time.Parse(layout, value)
		if err == nil {
			return instant, nil
		}
	}
	return time.Time{}, err
}

func formatTemporalValue(value time.Time, datatype ossie.DataType) string {
	switch datatype {
	case ossie.DataTypeDate:
		return value.Format("2006-01-02")
	case ossie.DataTypeTime:
		return value.Format("15:04:05.999999999")
	case ossie.DataTypeDateTime:
		return value.Format("2006-01-02T15:04:05.999999999")
	case ossie.DataTypeDateTimeTz:
		return value.Format(time.RFC3339Nano)
	default:
		return ""
	}
}

// snapshotResultValue copies the explicit Agent-facing value domain. Unknown
// driver objects are rejected rather than leaked or reflectively copied.
func snapshotResultValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return typed, nil
	case float32:
		if math.IsInf(float64(typed), 0) || math.IsNaN(float64(typed)) {
			return nil, fmt.Errorf("non-finite float32 result")
		}
		return typed, nil
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return nil, fmt.Errorf("non-finite float64 result")
		}
		return typed, nil
	case json.Number:
		if _, err := json.Marshal(typed); err != nil {
			return nil, fmt.Errorf("invalid json.Number result")
		}
		return typed, nil
	case []byte:
		return append([]byte(nil), typed...), nil
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			copy, err := snapshotResultValue(item)
			if err != nil {
				return nil, err
			}
			items[index] = copy
		}
		return items, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []int:
		return append([]int(nil), typed...), nil
	case map[string]any:
		fields := make(map[string]any, len(typed))
		for key, value := range typed {
			copy, err := snapshotResultValue(value)
			if err != nil {
				return nil, err
			}
			fields[key] = copy
		}
		return fields, nil
	case map[string]string:
		fields := make(map[string]string, len(typed))
		for key, value := range typed {
			fields[key] = value
		}
		return fields, nil
	default:
		return nil, fmt.Errorf("unsupported result value type %T", value)
	}
}

func normalizedText(value any) (string, error) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case []byte:
		text = string(typed)
	default:
		return "", fmt.Errorf("result is not text")
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("result is not valid UTF-8")
	}
	return text, nil
}

func normalizedNumericText(value any) (string, error) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case []byte:
		text = string(typed)
	case json.Number:
		text = typed.String()
	case int:
		text = strconv.FormatInt(int64(typed), 10)
	case int8:
		text = strconv.FormatInt(int64(typed), 10)
	case int16:
		text = strconv.FormatInt(int64(typed), 10)
	case int32:
		text = strconv.FormatInt(int64(typed), 10)
	case int64:
		text = strconv.FormatInt(typed, 10)
	case uint:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint8:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint16:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint32:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint64:
		text = strconv.FormatUint(typed, 10)
	case float32:
		text = strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		text = strconv.FormatFloat(typed, 'g', -1, 64)
	default:
		return "", fmt.Errorf("result is not numeric")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("result is not numeric")
	}
	return text, nil
}

func parseResultBoolean(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean result")
	}
}
