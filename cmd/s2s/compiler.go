package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
)

const OfflineProject = "s2s"

type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}

type Dimension struct {
	Name  string           `json:"name"`
	Grain *query.TimeGrain `json:"grain,omitempty"`
}

func (d *Dimension) UnmarshalJSON(data []byte) error {
	var compact string
	if err := json.Unmarshal(data, &compact); err == nil {
		d.Name = compact
		d.Grain = nil
		return nil
	}
	type dimensionAlias Dimension
	var structured dimensionAlias
	if err := json.Unmarshal(data, &structured); err != nil {
		return fmt.Errorf("dimension must be a string or object with name/grain: %w", err)
	}
	if strings.TrimSpace(structured.Name) == "" {
		return fmt.Errorf("dimension name is required")
	}
	*d = Dimension(structured)
	return nil
}

func NewDimension(name string) Dimension { return Dimension{Name: name} }

type QueryRequest struct {
	Model      string      `json:"model,omitempty"`
	Metrics    []string    `json:"metrics,omitempty"`
	Dimensions []Dimension `json:"dimensions,omitempty"`
	Filters    []Filter    `json:"filters,omitempty"`
	Limit      *int        `json:"limit,omitempty"`
}

type Result struct {
	Dialect      string
	Model        string
	Metrics      []string
	Dimensions   []string
	SQL          string
	Parameters   []sql.QueryParameter
	OutputSchema artifact.OutputSchema
}

func LoadRequestFile(path string) (QueryRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return QueryRequest{}, err
	}
	var req QueryRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		return QueryRequest{}, fmt.Errorf("decode request JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return QueryRequest{}, fmt.Errorf("request JSON must contain one object")
	}
	return req, nil
}

func LoadDocument(path string) (*ossie.Document, error) {
	return ossie.NewLoader().LoadFile(path)
}

func Compile(ctx context.Context, doc *ossie.Document, dialect string, req QueryRequest, strict bool) (*Result, error) {
	if doc == nil {
		return nil, fmt.Errorf("Ossie document is required")
	}
	modelName, err := resolveModelName(doc, req.Model)
	if err != nil {
		return nil, err
	}
	if strict {
		if err := validateStrict(doc, modelName); err != nil {
			return nil, err
		}
	}

	semanticManifest, err := manifest.BuildProjectManifest(OfflineProject, doc)
	if err != nil {
		return nil, err
	}
	store := manifest.NewStore(semanticManifest)
	semanticResolver := resolver.New(store)
	semanticPlanner := planner.New()

	queryRequest, err := toSemanticQuery(modelName, req)
	if err != nil {
		return nil, err
	}
	renderers, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		return nil, fmt.Errorf("create built-in Renderer registry: %w", err)
	}
	renderer, err := renderers.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		return nil, fmt.Errorf("SQL dialect %q is not supported: %w", dialect, err)
	}
	resolved, err := semanticResolver.ResolveForRenderer(ctx, queryRequest, renderer)
	if err != nil {
		return nil, err
	}
	plan, err := semanticPlanner.Plan(ctx, resolved, renderer)
	if err != nil {
		return nil, err
	}
	outputSchema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		return nil, err
	}

	compiled, err := compiler.CompileWithRenderer(ctx, plan, renderer)
	if err != nil {
		return nil, err
	}
	sqlQuery := compiled.SQLRenderResult
	dimensionNames := make([]string, 0, len(req.Dimensions))
	for _, dimension := range req.Dimensions {
		dimensionNames = append(dimensionNames, dimension.Name)
	}
	return &Result{Dialect: string(renderer.SQLDialect()), Model: modelName, Metrics: append([]string(nil), req.Metrics...), Dimensions: dimensionNames, SQL: sqlQuery.SQL, Parameters: sqlQuery.Parameters, OutputSchema: outputSchema}, nil
}

func resolveModelName(doc *ossie.Document, requested string) (string, error) {
	if requested != "" {
		for _, model := range doc.SemanticModel {
			if model.Name == requested {
				return requested, nil
			}
		}
		return "", fmt.Errorf("semantic model %q not found", requested)
	}
	if len(doc.SemanticModel) == 1 {
		return doc.SemanticModel[0].Name, nil
	}
	if len(doc.SemanticModel) == 0 {
		return "", fmt.Errorf("Ossie document contains no semantic models")
	}
	return "", fmt.Errorf("Ossie document contains %d semantic models; specify --semantic-model", len(doc.SemanticModel))
}

func toSemanticQuery(model string, req QueryRequest) (query.SemanticQuery, error) {
	q := query.SemanticQuery{Project: OfflineProject, Model: model, Limit: req.Limit}
	for _, name := range req.Metrics {
		if strings.TrimSpace(name) != "" {
			q.Metrics = append(q.Metrics, query.MetricRef{Name: strings.TrimSpace(name)})
		}
	}
	for _, dimension := range req.Dimensions {
		name := strings.TrimSpace(dimension.Name)
		if name != "" {
			q.Dimensions = append(q.Dimensions, query.DimensionRef{Name: name, Grain: dimension.Grain})
		}
	}
	for _, filter := range req.Filters {
		op, err := parseOperator(filter.Op)
		if err != nil {
			return query.SemanticQuery{}, err
		}
		q.Filters = append(q.Filters, query.Filter{Field: filter.Field, Operator: op, Value: filter.Value})
	}
	return q, nil
}

func parseOperator(op string) (query.FilterOperator, error) {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "=", "==", "eq":
		return query.FilterEQ, nil
	case "!=", "<>", "neq":
		return query.FilterNEQ, nil
	case ">", "gt":
		return query.FilterGT, nil
	case ">=", "gte":
		return query.FilterGTE, nil
	case "<", "lt":
		return query.FilterLT, nil
	case "<=", "lte":
		return query.FilterLTE, nil
	case "in":
		return query.FilterIN, nil
	case "not_in", "not in":
		return query.FilterNotIn, nil
	case "between":
		return query.FilterBetween, nil
	case "is_null", "is null":
		return query.FilterIsNull, nil
	case "is_not_null", "is not null":
		return query.FilterIsNotNull, nil
	default:
		return "", fmt.Errorf("unsupported filter operator %q", op)
	}
}

func validateStrict(doc *ossie.Document, modelName string) error {
	for _, model := range doc.SemanticModel {
		if model.Name != modelName {
			continue
		}
		for _, rel := range model.Relationships {
			if len(rel.FromColumns) == 0 || len(rel.FromColumns) != len(rel.ToColumns) {
				return fmt.Errorf("relationship %q has incomplete join columns", rel.Name)
			}
		}
		for _, metric := range model.Metrics {
			if len(metric.Expression.Dialects) == 0 {
				return fmt.Errorf("metric %q has no expression", metric.Name)
			}
		}
		return nil
	}
	return fmt.Errorf("semantic model %q not found", modelName)
}
