// Package regression evaluates project-owned semantic regression suites using
// the same application services as Metis's public transports.
package regression

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"go.yaml.in/yaml/v3"
)

const (
	SuiteSchemaVersion = 1
	MaxSuiteBytes      = 1 << 20
	MaxCases           = 100
)

var numericFilterLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// Suite is the strict, versioned compile or metric-result input.
type Suite struct {
	SchemaVersion int      `json:"schema_version" yaml:"schema_version"`
	Project       string   `json:"project" yaml:"project"`
	Cases         []Case   `json:"cases" yaml:"cases"`
	Fixture       *Fixture `json:"fixture,omitempty" yaml:"fixture"`
}

type Fixture struct {
	ID   string `json:"id" yaml:"id"`
	Kind string `json:"kind" yaml:"kind"`
}

type Case struct {
	ID        string       `json:"id" yaml:"id"`
	Operation string       `json:"operation" yaml:"operation"`
	Request   QueryRequest `json:"request" yaml:"request"`
	Expect    Expectation  `json:"expect" yaml:"expect"`
}

type QueryRequest struct {
	Query QueryInput `json:"query" yaml:"query"`
}

// QueryInput mirrors the public semantic request with explicit YAML keys. It
// prevents the shared request's permissive JSON decoding from weakening suite
// input validation.
type QueryInput struct {
	Project    string            `json:"project" yaml:"project"`
	Model      string            `json:"model" yaml:"model"`
	Intent     query.QueryIntent `json:"intent,omitempty" yaml:"intent"`
	Metrics    []MetricInput     `json:"metrics,omitempty" yaml:"metrics"`
	Dimensions []DimensionInput  `json:"dimensions,omitempty" yaml:"dimensions"`
	Filters    []FilterInput     `json:"filters,omitempty" yaml:"filters"`
	OrderBy    []OrderInput      `json:"order_by,omitempty" yaml:"order_by"`
	Limit      *int              `json:"limit,omitempty" yaml:"limit"`
}

type MetricInput struct {
	Name string `json:"name" yaml:"name"`
}

type DimensionInput struct {
	Name  string           `json:"name" yaml:"name"`
	Grain *query.TimeGrain `json:"grain,omitempty" yaml:"grain"`
}

type FilterInput struct {
	Field    string               `json:"field" yaml:"field"`
	Operator query.FilterOperator `json:"operator" yaml:"operator"`
	Value    any                  `json:"value,omitempty" yaml:"value"`
}

type OrderInput struct {
	Field     string              `json:"field" yaml:"field"`
	Direction query.SortDirection `json:"direction" yaml:"direction"`
}

type Expectation struct {
	Outcome         string          `json:"outcome" yaml:"outcome"`
	OutputSchema    *ExpectedSchema `json:"output_schema,omitempty" yaml:"output_schema"`
	Warnings        *[]ExpectedWarn `json:"warnings,omitempty" yaml:"warnings"`
	SQLRenderResult *ExpectedSQL    `json:"sql_render_result,omitempty" yaml:"sql_render_result"`
	Code            string          `json:"code,omitempty" yaml:"code"`
	CallerAction    string          `json:"caller_action,omitempty" yaml:"caller_action"`
	RowCount        *int64          `json:"row_count,omitempty" yaml:"row_count"`
	Rows            *ExpectedRows   `json:"rows,omitempty" yaml:"rows"`
}

type ExpectedSchema struct {
	Columns []ExpectedColumn `json:"columns" yaml:"columns"`
}

type ExpectedColumn struct {
	Name     string           `json:"name" yaml:"name"`
	Kind     string           `json:"kind" yaml:"kind"`
	Datatype string           `json:"datatype" yaml:"datatype"`
	Grain    *query.TimeGrain `json:"grain,omitempty" yaml:"grain"`
}

type ExpectedWarn struct {
	Code            string `json:"code" yaml:"code"`
	AssetRef        string `json:"asset_ref" yaml:"asset_ref"`
	DeprecationDate string `json:"deprecation_date,omitempty" yaml:"deprecation_date"`
	Replacement     string `json:"replacement,omitempty" yaml:"replacement"`
}

type ExpectedSQL struct {
	CompilerVersion string              `json:"compiler_version" yaml:"compiler_version"`
	Dialect         string              `json:"dialect" yaml:"dialect"`
	SQL             string              `json:"sql" yaml:"sql"`
	Parameters      []ExpectedParameter `json:"parameters,omitempty" yaml:"parameters"`
}

// Type distinguishes values that encode identically in JSON, such as an
// integer and a whole-valued float. Decimal values use an exact string.
type ExpectedParameter struct {
	Name  string `json:"name,omitempty" yaml:"name"`
	Type  string `json:"type" yaml:"type"`
	Value any    `json:"value" yaml:"value"`
}

func LoadSuite(path string) (Suite, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return Suite{}, "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxSuiteBytes+1))
	if err != nil {
		return Suite{}, "", err
	}
	if len(data) > MaxSuiteBytes {
		return Suite{}, "", fmt.Errorf("suite exceeds %d bytes", MaxSuiteBytes)
	}
	return ParseSuite(data)
}

func ParseSuite(data []byte) (Suite, string, error) {
	if len(data) > MaxSuiteBytes {
		return Suite{}, "", fmt.Errorf("suite exceeds %d bytes", MaxSuiteBytes)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Suite{}, "", fmt.Errorf("decode suite: %w", err)
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return Suite{}, "", fmt.Errorf("suite must be one mapping document")
	}
	if err := validateYAMLNode(&node); err != nil {
		return Suite{}, "", err
	}
	if err := validateFilterLiterals(node.Content[0]); err != nil {
		return Suite{}, "", err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var suite Suite
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, "", fmt.Errorf("decode suite: %w", err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Suite{}, "", fmt.Errorf("suite must contain exactly one document")
	}
	if err := suite.validate(); err != nil {
		return Suite{}, "", err
	}
	digest := sha256.Sum256(data)
	return suite, hex.EncodeToString(digest[:]), nil
}

func validateYAMLNode(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("YAML aliases are not supported in suites")
	}
	if node.Kind == yaml.ScalarNode {
		switch node.Tag {
		case "!!str", "!!int", "!!float", "!!bool", "!!null":
		default:
			return fmt.Errorf("unsupported YAML scalar tag %q", node.Tag)
		}
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("suite keys must be strings")
			}
			if seen[key.Value] {
				return fmt.Errorf("duplicate suite key %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLNode(child); err != nil {
			return err
		}
	}
	return nil
}

func (s Suite) validate() error {
	if s.SchemaVersion != SuiteSchemaVersion {
		return fmt.Errorf("unsupported suite schema_version %d", s.SchemaVersion)
	}
	if strings.TrimSpace(s.Project) == "" {
		return fmt.Errorf("suite project is required")
	}
	if len(s.Cases) == 0 || len(s.Cases) > MaxCases {
		return fmt.Errorf("suite must contain 1 to %d cases", MaxCases)
	}
	operation := "compile_sql"
	if s.Fixture != nil {
		if strings.TrimSpace(s.Fixture.ID) == "" || (s.Fixture.Kind != "externally_prepared" && s.Fixture.Kind != "live") {
			return fmt.Errorf("runtime fixture requires id and kind externally_prepared or live")
		}
		operation = "query_metrics"
	}
	ids := make(map[string]bool, len(s.Cases))
	for _, c := range s.Cases {
		if strings.TrimSpace(c.ID) == "" || ids[c.ID] {
			return fmt.Errorf("case ID must be nonempty and unique: %q", c.ID)
		}
		ids[c.ID] = true
		if c.Operation != operation {
			return fmt.Errorf("case %q: expected %s operation for this suite", c.ID, operation)
		}
		if c.Request.Query.Project != s.Project || strings.TrimSpace(c.Request.Query.Model) == "" {
			return fmt.Errorf("case %q: request project must match suite and model is required", c.ID)
		}
		if _, err := c.Request.Query.semanticQuery(); err != nil {
			return fmt.Errorf("case %q: %w", c.ID, err)
		}
		if err := c.Expect.validate(); err != nil {
			return fmt.Errorf("case %q: %w", c.ID, err)
		}
		if s.Fixture == nil {
			if c.Expect.Rows != nil || c.Expect.RowCount != nil {
				return fmt.Errorf("case %q: compile suites cannot assert result rows", c.ID)
			}
		} else if err := validateRuntimeCase(c); err != nil {
			return fmt.Errorf("case %q: %w", c.ID, err)
		}
	}
	return nil
}

func (e Expectation) validate() error {
	switch e.Outcome {
	case "success":
		if e.OutputSchema == nil || len(e.OutputSchema.Columns) == 0 {
			return fmt.Errorf("success requires a nonempty exact output_schema")
		}
		if e.Code != "" || e.CallerAction != "" {
			return fmt.Errorf("success cannot expect an error")
		}
		for _, col := range e.OutputSchema.Columns {
			if col.Name == "" || (col.Kind != string(artifact.OutputDimension) && col.Kind != string(artifact.OutputMetric)) || col.Datatype == "" {
				return fmt.Errorf("output_schema columns require name, kind, and datatype")
			}
		}
		if e.SQLRenderResult != nil && (e.SQLRenderResult.CompilerVersion == "" || e.SQLRenderResult.Dialect == "" || strings.TrimSpace(e.SQLRenderResult.SQL) == "") {
			return fmt.Errorf("SQL snapshot requires compiler_version, dialect, and SQL")
		}
		if e.SQLRenderResult != nil {
			for _, param := range e.SQLRenderResult.Parameters {
				if err := param.validate(); err != nil {
					return err
				}
			}
		}
		if e.Warnings != nil {
			for _, warning := range *e.Warnings {
				if warning.Code == "" || warning.AssetRef == "" {
					return fmt.Errorf("warning assertions require code and asset_ref")
				}
			}
		}
	case "semantic_error":
		if e.OutputSchema != nil || e.Warnings != nil || e.SQLRenderResult != nil || e.Rows != nil || e.RowCount != nil {
			return fmt.Errorf("semantic_error cannot include success assertions")
		}
		if e.Code == "" || e.CallerAction == "" || e.Code == string(serrors.ErrProjectAccessDenied) || e.Code == string(serrors.ErrDataAccessDenied) {
			return fmt.Errorf("semantic_error requires an allowed stable code and caller_action")
		}
		found := false
		for _, code := range serrors.Codes() {
			if string(code) == e.Code {
				found = true
				break
			}
		}
		if !found || string(serrors.CallerActionOf(serrors.ErrorCode(e.Code))) != e.CallerAction || e.CallerAction == string(serrors.CallerActionReportDefect) {
			return fmt.Errorf("semantic_error code and caller_action must match a client-actionable registered error")
		}
	default:
		return fmt.Errorf("unsupported expected outcome %q", e.Outcome)
	}
	return nil
}

func (p ExpectedParameter) validate() error {
	switch p.Type {
	case "null":
		if p.Value != nil {
			return fmt.Errorf("null parameter must have null value")
		}
	case "string", "decimal", "bytes":
		if _, ok := p.Value.(string); !ok {
			return fmt.Errorf("%s parameter requires a string value", p.Type)
		}
	case "bool":
		if _, ok := p.Value.(bool); !ok {
			return fmt.Errorf("bool parameter requires a boolean value")
		}
	case "integer", "float":
		if p.Type == "integer" {
			switch p.Value.(type) {
			case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			default:
				return fmt.Errorf("integer parameter requires an exact integer value")
			}
		} else {
			switch value := p.Value.(type) {
			case float32:
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					return fmt.Errorf("float parameter must be finite")
				}
			case float64:
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return fmt.Errorf("float parameter must be finite")
				}
			default:
				return fmt.Errorf("float parameter requires a floating-point value")
			}
		}
	default:
		return fmt.Errorf("unsupported parameter type %q", p.Type)
	}
	return nil
}

func (q QueryInput) semanticQuery() (query.SemanticQuery, error) {
	out := query.SemanticQuery{Project: q.Project, Model: q.Model, Intent: q.Intent, Limit: q.Limit}
	for _, metric := range q.Metrics {
		out.Metrics = append(out.Metrics, query.MetricRef{Name: metric.Name})
	}
	for _, dimension := range q.Dimensions {
		out.Dimensions = append(out.Dimensions, query.DimensionRef{Name: dimension.Name, Grain: dimension.Grain})
	}
	for _, order := range q.OrderBy {
		out.OrderBy = append(out.OrderBy, query.OrderBy{Field: order.Field, Direction: order.Direction})
	}
	for _, filter := range q.Filters {
		encoded, err := json.Marshal(filter)
		if err != nil {
			return query.SemanticQuery{}, err
		}
		var original any
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&original); err != nil {
			return query.SemanticQuery{}, err
		}
		if err := checkFilterNumbers(original); err != nil {
			return query.SemanticQuery{}, err
		}
		var checked query.Filter
		if err := json.Unmarshal(encoded, &checked); err != nil {
			return query.SemanticQuery{}, err
		}
		out.Filters = append(out.Filters, checked)
	}
	return out, nil
}

// Public filter decoding currently uses float64. Do not change that API here:
// refuse literals whose decimal value changes through its JSON round trip.
func checkFilterNumber(text string) error {
	if !json.Valid([]byte(text)) {
		return fmt.Errorf("filter numbers require JSON decimal syntax without YAML base prefixes or leading zeros")
	}
	want, err := exactNumber(text)
	if err != nil {
		return fmt.Errorf("filter numbers require bounded finite decimal literals")
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("filter number is outside the supported range")
	}
	if want.IsInt() && want.Cmp(new(big.Rat).SetFloat64(number)) != 0 {
		return fmt.Errorf("integer filter loses precision in the query API")
	}
	encoded, err := json.Marshal(number)
	if err != nil {
		return fmt.Errorf("filter number must be finite")
	}
	got, err := exactNumber(string(encoded))
	if err != nil || want.Cmp(got) != 0 {
		return fmt.Errorf("filter number loses precision in the query API; use a supported exact value")
	}
	return nil
}

func checkFilterNumbers(value any) error {
	switch v := value.(type) {
	case json.Number:
		return checkFilterNumber(string(v))
	case []any:
		for _, item := range v {
			if err := checkFilterNumbers(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range v {
			if err := checkFilterNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// Inspect original YAML/JSON tokens before YAML decoding could itself round a
// decimal. Only numeric filter values are subject to this public-API boundary.
func validateFilterLiterals(root *yaml.Node) error {
	field := func(n *yaml.Node, name string) *yaml.Node {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == name {
				return n.Content[i+1]
			}
		}
		return nil
	}
	var check func(*yaml.Node) error
	check = func(n *yaml.Node) error {
		if n == nil {
			return nil
		}
		if n.Kind == yaml.ScalarNode && (n.Tag == "!!int" || n.Tag == "!!float" || (n.Tag == "!!str" && n.Style == 0 && numericFilterLiteral.MatchString(n.Value))) {
			return checkFilterNumber(n.Value)
		}
		for _, child := range n.Content {
			if err := check(child); err != nil {
				return err
			}
		}
		return nil
	}
	cases := field(root, "cases")
	if cases == nil {
		return nil
	}
	for i, c := range cases.Content {
		filters := field(field(field(c, "request"), "query"), "filters")
		if filters == nil {
			continue
		}
		for j, f := range filters.Content {
			if err := check(field(f, "value")); err != nil {
				return fmt.Errorf("case %d filter %d: %w", i, j, err)
			}
		}
	}
	return nil
}
