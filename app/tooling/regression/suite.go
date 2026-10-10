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
	"os"
	"regexp"
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
	Filters    *PredicateInput   `json:"filters,omitempty" yaml:"filters"`
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

type PredicateInput struct {
	Kind     string           `json:"kind" yaml:"kind"`
	Filter   *FilterInput     `json:"filter,omitempty" yaml:"filter"`
	Children []PredicateInput `json:"children,omitempty" yaml:"children"`
	Child    *PredicateInput  `json:"child,omitempty" yaml:"child"`
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
	if err := restoreSuiteFilterValues(node.Content[0], &suite); err != nil {
		return Suite{}, "", err
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
	if q.Filters != nil {
		encoded, err := json.Marshal(q.Filters)
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
		var checked query.Predicate
		if err := json.Unmarshal(encoded, &checked); err != nil {
			return query.SemanticQuery{}, err
		}
		out.Filters = checked
	}
	return out, nil
}

// Suite YAML tokens and public JSON requests use the same numeric syntax.
func checkFilterNumber(text string) error {
	_, err := query.ParseFilterNumber(text)
	return err
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

// restoreSuiteFilterValues reads numeric tokens from the YAML syntax tree after
// strict typed decoding. This prevents YAML's interface decoding from rounding
// a number before the shared query decoder can preserve it as json.Number.
func restoreSuiteFilterValues(root *yaml.Node, suite *Suite) error {
	cases := yamlField(root, "cases")
	if cases == nil || suite == nil || len(cases.Content) != len(suite.Cases) {
		return nil
	}
	for i, c := range cases.Content {
		filters := yamlField(yamlField(yamlField(c, "request"), "query"), "filters")
		if filters == nil || suite.Cases[i].Request.Query.Filters == nil {
			continue
		}
		if err := restorePredicateFilterValues(filters, suite.Cases[i].Request.Query.Filters); err != nil {
			return fmt.Errorf("case %d filters: %w", i, err)
		}
	}
	return nil
}

func restorePredicateFilterValues(node *yaml.Node, predicate *PredicateInput) error {
	if node == nil || predicate == nil {
		return nil
	}
	switch predicate.Kind {
	case "filter":
		valueNode := yamlField(yamlField(node, "filter"), "value")
		if valueNode == nil || predicate.Filter == nil {
			return nil
		}
		value, err := decodeFilterYAMLValue(valueNode)
		if err != nil {
			return err
		}
		predicate.Filter.Value = value
	case "and", "or":
		children := yamlField(node, "children")
		if children == nil || children.Kind != yaml.SequenceNode || len(children.Content) != len(predicate.Children) {
			return nil
		}
		for i := range predicate.Children {
			if err := restorePredicateFilterValues(children.Content[i], &predicate.Children[i]); err != nil {
				return err
			}
		}
	case "not":
		return restorePredicateFilterValues(yamlField(node, "child"), predicate.Child)
	}
	return nil
}

func decodeFilterYAMLValue(node *yaml.Node) (any, error) {
	if node == nil || node.Tag == "!!null" {
		return nil, nil
	}
	if node.Kind == yaml.SequenceNode {
		out := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := decodeFilterYAMLValue(child)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	}
	if node.Kind == yaml.ScalarNode && (node.Tag == "!!int" || node.Tag == "!!float" || (node.Tag == "!!str" && node.Style == 0 && numericFilterLiteral.MatchString(node.Value))) {
		return query.ParseFilterNumber(node.Value)
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func yamlField(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}
