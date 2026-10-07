package clickhouse

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/meaningforge/metis/renderer/sql"
)

// bindServerParameters changes only placeholder spelling, never inserts values
// into SQL. Production SELECT and validation EXPLAIN share this transport path.
func bindServerParameters(query sql.SqlRenderResult) (string, clickhousedriver.Parameters, error) {
	if len(query.Parameters) == 0 {
		return query.SQL, nil, nil
	}
	parameters := make(clickhousedriver.Parameters, len(query.Parameters))
	markers := make([]string, len(query.Parameters))
	for i, p := range query.Parameters {
		typeName, value, err := serverParameter(p.Value)
		if err != nil {
			return "", nil, err
		}
		name := fmt.Sprintf("metis_param_%d", i)
		markers[i] = "{" + name + ":" + typeName + "}"
		parameters[name] = value
	}
	var out strings.Builder
	text := query.SQL
	parameter := 0
	for i := 0; i < len(text); {
		start := i
		switch {
		case text[i] == '\'' || text[i] == '"' || text[i] == '`':
			quote := text[i]
			i++
			closed := false
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == quote {
					if i+1 < len(text) && text[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed || i > len(text) {
				return "", nil, fmt.Errorf("unsupported SQL quoting")
			}
		case text[i] == '#' || (i+1 < len(text) && (text[i:i+2] == "--" || text[i:i+2] == "//")):
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case i+1 < len(text) && text[i:i+2] == "/*":
			i += 2
			depth := 1
			for i < len(text) && depth > 0 {
				if i+1 < len(text) && text[i:i+2] == "/*" {
					depth++
					i += 2
				} else if i+1 < len(text) && text[i:i+2] == "*/" {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return "", nil, fmt.Errorf("unsupported SQL comment")
			}
		case text[i] == '?':
			if parameter == len(markers) {
				return "", nil, fmt.Errorf("parameter count mismatch")
			}
			out.WriteString(markers[parameter])
			parameter++
			i++
			continue
		case text[i] == '{' || text[i] == '}':
			return "", nil, fmt.Errorf("unmanaged server parameter syntax")
		default:
			i++
		}
		out.WriteString(text[start:i])
	}
	if parameter != len(markers) {
		return "", nil, fmt.Errorf("parameter count mismatch")
	}
	return out.String(), parameters, nil
}

func serverParameter(value any) (string, string, error) {
	switch v := value.(type) {
	case nil:
		return "Nullable(String)", `\N`, nil
	case string:
		return "String", escapeParameterString(v), nil
	case []byte:
		return "String", escapeParameterString(string(v)), nil
	case bool:
		return "Bool", strconv.FormatBool(v), nil
	case int:
		return "Int64", strconv.FormatInt(int64(v), 10), nil
	case int8:
		return "Int8", strconv.FormatInt(int64(v), 10), nil
	case int16:
		return "Int16", strconv.FormatInt(int64(v), 10), nil
	case int32:
		return "Int32", strconv.FormatInt(int64(v), 10), nil
	case int64:
		return "Int64", strconv.FormatInt(v, 10), nil
	case uint:
		return "UInt64", strconv.FormatUint(uint64(v), 10), nil
	case uint8:
		return "UInt8", strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return "UInt16", strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return "UInt32", strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return "UInt64", strconv.FormatUint(v, 10), nil
	case float32:
		if !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) {
			return "Float32", strconv.FormatFloat(float64(v), 'g', -1, 32), nil
		}
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return "Float64", strconv.FormatFloat(v, 'g', -1, 64), nil
		}
	case json.Number:
		if n, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return "Int64", strconv.FormatInt(n, 10), nil
		}
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return "UInt64", strconv.FormatUint(n, 10), nil
		}
		// No float conversion of exact decimal evidence. Unsupported exponent or
		// excessively wide decimals remain explicit until a typed contract exists.
		s := string(v)
		parts := strings.Split(s, ".")
		if len(parts) == 2 && !strings.ContainsAny(s, "eE+") && len(strings.TrimPrefix(parts[0], "-"))+len(parts[1]) <= 76 && len(parts[1]) > 0 {
			if _, err := json.Marshal(v); err == nil {
				return fmt.Sprintf("Decimal(76,%d)", len(parts[1])), s, nil
			}
		}
	}
	return "", "", fmt.Errorf("unsupported server parameter type")
}

// HTTP parameter values use the server's Escaped text format independently of
// URL encoding. Escaping is transport serialization, never SQL interpolation.
func escapeParameterString(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\x00", "\\0", "\n", "\\n", "\r", "\\r", "\t", "\\t", "\b", "\\b", "\f", "\\f").Replace(value)
}
