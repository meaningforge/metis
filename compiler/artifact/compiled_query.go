package artifact

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/meaningforge/metis/renderer/sql"
)

// CompiledQuery is the atomic, target-neutral compiler artifact consumed by
// execution Runner. It deliberately contains no DataSource, Backend, Renderer,
// or routing state.
type CompiledQuery struct {
	SQLRenderResult sql.SQLRenderResult `json:"render_result"`
	OutputSchema    OutputSchema        `json:"output_schema"`
	Warnings        []Warning           `json:"warnings,omitempty"`
}

type Warning struct {
	Code            string `json:"code"`
	AssetRef        string `json:"asset_ref"`
	DeprecationDate string `json:"deprecation_date,omitempty"`
	Replacement     string `json:"replacement,omitempty"`
}

// NewCompiledQuery establishes ownership of a compiler artifact at its
// construction boundary. Its nested containers do not alias Renderer-owned
// storage.
func NewCompiledQuery(query sql.SQLRenderResult, schema OutputSchema) (*CompiledQuery, error) {
	return SnapshotCompiledQuery(&CompiledQuery{SQLRenderResult: query, OutputSchema: schema})
}

// SnapshotCompiledQuery copies known containers and rejects values outside the
// closed compiler artifact domain. It deliberately does not attempt reflective
// or generic deep copying.
func SnapshotCompiledQuery(compiled *CompiledQuery) (*CompiledQuery, error) {
	if compiled == nil {
		return nil, nil
	}
	if compiled.SQLRenderResult.Dialect == "" {
		return nil, fmt.Errorf("physical SQL query dialect is required")
	}
	if strings.TrimSpace(compiled.SQLRenderResult.SQL) == "" {
		return nil, fmt.Errorf("physical SQL query text is required")
	}
	query, err := snapshotSQLRenderResult(compiled.SQLRenderResult)
	if err != nil {
		return nil, err
	}
	return &CompiledQuery{
		SQLRenderResult: query,
		OutputSchema:    copyOutputSchema(compiled.OutputSchema),
		Warnings:        append([]Warning(nil), compiled.Warnings...),
	}, nil
}

func snapshotSQLRenderResult(query sql.SQLRenderResult) (sql.SQLRenderResult, error) {
	snapshot := query
	snapshot.Parameters = make([]sql.QueryParameter, len(query.Parameters))
	for index, parameter := range query.Parameters {
		value, err := snapshotQueryParameterValue(parameter.Value)
		if err != nil {
			return sql.SQLRenderResult{}, fmt.Errorf("parameter %d: %w", index, err)
		}
		snapshot.Parameters[index] = sql.QueryParameter{Name: parameter.Name, Value: value}
	}
	return snapshot, nil
}

// snapshotQueryParameterValue is the ownership implementation of
// QueryParameter's closed value domain. Scalar values are immutable; []byte is
// the sole mutable parameter value and is copied explicitly.
func snapshotQueryParameterValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return typed, nil
	case float32:
		if math.IsInf(float64(typed), 0) || math.IsNaN(float64(typed)) {
			return nil, fmt.Errorf("non-finite float32 is not supported")
		}
		return typed, nil
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return nil, fmt.Errorf("non-finite float64 is not supported")
		}
		return typed, nil
	case json.Number:
		if _, err := json.Marshal(typed); err != nil {
			return nil, fmt.Errorf("invalid json.Number")
		}
		return typed, nil
	case []byte:
		return append([]byte(nil), typed...), nil
	default:
		return nil, fmt.Errorf("unsupported query parameter value type %T", value)
	}
}

func copyOutputSchema(schema OutputSchema) OutputSchema {
	columns := make([]OutputColumn, len(schema.Columns))
	copy(columns, schema.Columns)
	for index := range columns {
		if schema.Columns[index].Grain != nil {
			grain := *schema.Columns[index].Grain
			columns[index].Grain = &grain
		}
	}
	return OutputSchema{Columns: columns}
}
