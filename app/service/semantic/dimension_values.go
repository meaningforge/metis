package semantic

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

const (
	defaultDimensionValuesLimit = 100
	maxDimensionValuesLimit     = 500
	maxDimensionValuesMetrics   = 8
)

// DimensionValuesQuery is the closed semantic request for bounded live
// dimension-member discovery. It deliberately contains no SQL, filters,
// ordering, dialect, DataSource, Renderer, Driver, or runtime controls.
type DimensionValuesQuery struct {
	ProjectID string           `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Dimension string           `json:"dimension" jsonschema:"Canonical dimension ref copied exactly from get_dimensions."`
	Metrics   []string         `json:"metrics,omitempty" jsonschema:"Optional canonical metric refs from list_metrics; every metric must be compatible with the dimension."`
	Grain     *query.TimeGrain `json:"grain,omitempty" jsonschema:"Optional valid grain for a canonical time dimension; custom calendars use the logical grain on canonical_groupings[].group_by."`
	Limit     *int             `json:"limit,omitempty" jsonschema:"Maximum public values; defaults to 100 and must be between 1 and 500."`

	projectContextInherited bool
}

func (q *DimensionValuesQuery) MarkProjectContextInherited(inherited bool) {
	if q != nil {
		q.projectContextInherited = inherited
	}
}

// DimensionValuesResult contains only normalized values for the selected
// dimension. Count is the returned-page size, not total warehouse cardinality.
type DimensionValuesResult struct {
	QueryID   string             `json:"query_id"`
	Dimension string             `json:"dimension"`
	Grain     *query.TimeGrain   `json:"grain,omitempty"`
	DataType  ossie.DataType     `json:"data_type"`
	Values    []any              `json:"values"`
	Count     int                `json:"count"`
	Truncated bool               `json:"truncated"`
	Warnings  []artifact.Warning `json:"warnings,omitempty"`
}

// DimensionValuesService composes one closed semantic query through the same
// project, Backend Renderer, compiler, and Runner authorities as query_metrics.
type DimensionValuesService struct {
	compile               *CompileService
	discovery             *DiscoveryService
	projects              ExecutionProjectResolver
	runtime               *runner.Runner
	authorizer            ProjectAuthorizer
	authorizationObserver ProjectAuthorizationObserver
}

func NewDimensionValuesService(compile *CompileService, discovery *DiscoveryService, projects ExecutionProjectResolver, executionRuntime *runner.Runner) *DimensionValuesService {
	return &DimensionValuesService{
		compile: compile, discovery: discovery, projects: projects, runtime: executionRuntime,
		authorizer: ScopeProjectAuthorizer{},
	}
}

func (s *DimensionValuesService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *DimensionValuesService {
	if s != nil {
		s.authorizer = authorizer
	}
	return s
}

func (s *DimensionValuesService) WithProjectAuthorizationObserver(observer ProjectAuthorizationObserver) *DimensionValuesService {
	if s != nil {
		s.authorizationObserver = observer
	}
	return s
}

func (s *DimensionValuesService) GetDimensionValues(ctx context.Context, input DimensionValuesQuery) (*DimensionValuesResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "dimension value execution is not configured")
	}
	if s.projects == nil {
		if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, input.ProjectID, ProjectActionExecute); err != nil {
			return nil, err
		}
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "dimension value execution is not configured")
	}

	project, err := s.projects.ResolveProject(input.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, project, ProjectActionExecute); err != nil {
		return nil, err
	}
	if s.compile == nil || s.discovery == nil || s.runtime == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "dimension value execution is not configured")
	}
	input.ProjectID = project
	normalized, err := s.normalizeDimensionValues(ctx, input)
	if err != nil {
		return nil, err
	}
	semanticQuery := normalized.semanticQuery()
	if s.compile.discovery != nil {
		if err := s.compile.discovery.authorizeSemanticQueryAssets(ctx, semanticQuery, ProjectActionExecute); err != nil {
			return nil, err
		}
	}

	queryID, err := newQueryID()
	if err != nil {
		return nil, serrors.Internal("could not create query identifier", nil)
	}
	executionCtx := runner.WithQueryID(ctx, queryID)
	dataSource, ok := dataSourceForModel(s.projects, project, normalized.model)
	if !ok {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "semantic model has no configured DataSource")
	}
	route, err := s.runtime.ResolveDataSource(dataSource)
	if err != nil {
		return nil, mapExecutionError(err)
	}
	compiled, err := s.compile.compileWithRenderer(executionCtx, CompileRequest{
		Query:                   semanticQuery,
		ProjectContextInherited: input.projectContextInherited,
	}, route.Backend.Renderer)
	if err != nil {
		return nil, err
	}
	if err := validateDimensionValuesSchema(compiled.OutputSchema, normalized); err != nil {
		return nil, err
	}

	result, err := s.runtime.ExecuteResolved(executionCtx, route, compiled, runner.ExecutionOptions{MaxRows: int64(normalized.limit + 1)})
	if err != nil {
		return nil, mapExecutionError(err)
	}
	values, truncated, dataType, err := projectDimensionValues(result, compiled.OutputSchema, normalized)
	if err != nil {
		return nil, err
	}
	return &DimensionValuesResult{
		QueryID: queryID, Dimension: normalized.publicDimension, Grain: cloneTimeGrain(normalized.grain),
		DataType: dataType, Values: values, Count: len(values), Truncated: truncated,
		Warnings: append([]artifact.Warning(nil), compiled.Warnings...),
	}, nil
}

type normalizedDimensionValues struct {
	project, model, dimension, publicDimension string
	metrics                                    []normalizedDimensionValueMetric
	grain                                      *query.TimeGrain
	limit                                      int
}

type normalizedDimensionValueMetric struct {
	public   string
	internal string
}

func (s *DimensionValuesService) normalizeDimensionValues(ctx context.Context, input DimensionValuesQuery) (normalizedDimensionValues, error) {
	if s.discovery.manifest == nil {
		return normalizedDimensionValues{}, serrors.Internal("semantic manifest is not configured", nil)
	}
	snapshot := s.discovery.manifest.Current()
	if snapshot == nil {
		return normalizedDimensionValues{}, serrors.Internal("semantic manifest is not loaded", nil)
	}
	project, err := snapshot.Project(input.ProjectID)
	if err != nil {
		return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found"}
	}

	rawDimension := strings.TrimSpace(input.Dimension)
	if rawDimension == "" || !strings.HasPrefix(rawDimension, "dimension:") {
		return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "get_dimension_values requires a canonical dimension ref"}
	}
	model, dimension, handle, err := resolveAgentDimensionRef(project, rawDimension)
	if err != nil {
		return normalizedDimensionValues{}, err
	}
	canonicalDimension := "dimension:" + model + "." + dimension
	if rawDimension != canonicalDimension {
		return normalizedDimensionValues{}, agentDimensionNotFoundError(rawDimension)
	}
	if !supportedDimensionValuesDataType(handle.Field.Datatype) {
		return normalizedDimensionValues{}, &serrors.Error{
			Code: serrors.ErrInvalidQuery, Message: "dimension datatype is not supported for value discovery",
			Details: map[string]any{"dimension": canonicalDimension, "data_type": handle.Field.Datatype},
		}
	}
	dimensionType := AgentGroupByDimension
	if isAgentTimeDimension(handle.Field) {
		dimensionType = AgentGroupByTimeDimension
	}
	if err := validateAgentGroupBy(model, project.Models[model], dimension, AgentGroupByParam{Name: canonicalDimension, Type: dimensionType, Grain: input.Grain}); err != nil {
		return normalizedDimensionValues{}, err
	}

	limit := defaultDimensionValuesLimit
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > maxDimensionValuesLimit {
		return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "dimension value limit must be between 1 and 500", Details: map[string]any{"limit": limit}}
	}
	if len(input.Metrics) > maxDimensionValuesMetrics {
		return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "get_dimension_values accepts at most eight metrics"}
	}

	normalized := normalizedDimensionValues{
		project: input.ProjectID, model: model, dimension: dimension, publicDimension: canonicalDimension,
		grain: cloneTimeGrain(input.Grain), limit: limit,
	}
	seenMetrics := make(map[string]struct{}, len(input.Metrics))
	metricNames := make([]string, 0, len(input.Metrics))
	for _, raw := range input.Metrics {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.HasPrefix(raw, "metric:") {
			return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "get_dimension_values requires canonical metric refs"}
		}
		metricModel, metric, resolveErr := resolveAgentMetricRef(project, raw)
		if resolveErr != nil {
			return normalizedDimensionValues{}, resolveErr
		}
		canonicalMetric := "metric:" + metricModel + "." + metric
		if raw != canonicalMetric {
			return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"metric": raw}}
		}
		if metricModel != model {
			return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "dimension and metrics must belong to one semantic model"}
		}
		if _, exists := seenMetrics[canonicalMetric]; exists {
			return normalizedDimensionValues{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "get_dimension_values metrics must be distinct"}
		}
		seenMetrics[canonicalMetric] = struct{}{}
		metricNames = append(metricNames, metric)
		normalized.metrics = append(normalized.metrics, normalizedDimensionValueMetric{public: canonicalMetric, internal: metric})
	}
	if len(metricNames) > 0 {
		compatibility, compatibilityErr := s.discovery.GetMetricsDimensions(ctx, GetMetricsDimensionsRequest{
			Project: input.ProjectID, Model: model, Metrics: metricNames,
		})
		if compatibilityErr != nil {
			return normalizedDimensionValues{}, compatibilityErr
		}
		if err := requireDimensionValueCompatibility(compatibility, dimension, len(metricNames)); err != nil {
			return normalizedDimensionValues{}, err
		}
	}
	return normalized, nil
}

func (n normalizedDimensionValues) semanticQuery() query.SemanticQuery {
	internalLimit := n.limit + 1
	semanticQuery := query.SemanticQuery{
		Project: n.project, Model: n.model,
		Dimensions: []query.DimensionRef{{Name: n.dimension, Grain: cloneTimeGrain(n.grain)}},
		Filters:    []query.Filter{{Field: n.dimension, Operator: query.FilterIsNotNull}},
		OrderBy:    []query.OrderBy{{Field: n.dimension, Direction: query.SortAsc}},
		Limit:      &internalLimit,
	}
	if len(n.metrics) == 0 {
		semanticQuery.Intent = query.QueryIntentDistinctValues
	} else {
		for _, metric := range n.metrics {
			semanticQuery.Metrics = append(semanticQuery.Metrics, query.MetricRef{Name: metric.internal})
		}
	}
	return semanticQuery
}

func requireDimensionValueCompatibility(result *MetricSetDimensionCompatibilityResult, qualified string, metricCount int) error {
	if result != nil {
		for _, dimension := range result.Dimensions {
			if dimension.Qualified != qualified {
				continue
			}
			if dimension.Status == CompatibilityAmbiguous {
				return &serrors.Error{Code: serrors.ErrAmbiguousRelationshipPath, Message: "dimension compatibility is ambiguous", Details: map[string]any{"dimension": qualified}}
			}
			if dimension.Status == CompatibilityCompatible && len(dimension.PerMetric) == metricCount {
				for _, evidence := range dimension.PerMetric {
					if evidence.Status != CompatibilityCompatible {
						return incompatibleDimensionValuesMetric(qualified)
					}
				}
				return nil
			}
			return incompatibleDimensionValuesMetric(qualified)
		}
	}
	return incompatibleDimensionValuesMetric(qualified)
}

func incompatibleDimensionValuesMetric(dimension string) error {
	return &serrors.Error{
		Code: serrors.ErrMetricSourceUnreachable, Message: "dimension is not compatible with every selected metric",
		Details: map[string]any{"dimension": dimension},
	}
}

func validateDimensionValuesSchema(schema artifact.OutputSchema, normalized normalizedDimensionValues) error {
	wantColumns := 1 + len(normalized.metrics)
	if len(schema.Columns) != wantColumns {
		return inconsistentDimensionValuesResult("compiled output column count is inconsistent")
	}
	dimension := schema.Columns[0]
	if dimension.Name != normalized.dimension || dimension.Kind != artifact.OutputDimension || !sameTimeGrain(dimension.Grain, normalized.grain) || !supportedDimensionValuesDataType(dimension.Datatype) {
		return inconsistentDimensionValuesResult("compiled dimension output is inconsistent")
	}
	for index, metric := range normalized.metrics {
		column := schema.Columns[index+1]
		if column.Name != metric.internal || column.Kind != artifact.OutputMetric || column.Grain != nil {
			return inconsistentDimensionValuesResult("compiled metric output is inconsistent")
		}
	}
	return nil
}

func projectDimensionValues(result runner.ResultSet, compiledSchema artifact.OutputSchema, normalized normalizedDimensionValues) ([]any, bool, ossie.DataType, error) {
	if !sameOutputSchema(result.Schema, compiledSchema) || result.Count != int64(len(result.Rows)) || result.Count > int64(normalized.limit+1) {
		return nil, false, "", inconsistentDimensionValuesResult("executed result shape is inconsistent")
	}
	values := make([]any, 0, min(len(result.Rows), normalized.limit))
	seen := make(map[string]struct{}, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) != len(compiledSchema.Columns) || row[0] == nil || !validNormalizedDimensionValue(row[0], compiledSchema.Columns[0].Datatype) {
			return nil, false, "", inconsistentDimensionValuesResult("executed dimension value is inconsistent")
		}
		key, ok := normalizedDimensionValueKey(row[0], compiledSchema.Columns[0].Datatype)
		if !ok {
			return nil, false, "", inconsistentDimensionValuesResult("executed dimension value cannot be compared")
		}
		if _, exists := seen[key]; exists {
			return nil, false, "", inconsistentDimensionValuesResult("executed dimension values contain duplicates")
		}
		seen[key] = struct{}{}
		if len(values) < normalized.limit {
			values = append(values, row[0])
		}
	}
	if values == nil {
		values = []any{}
	}
	return values, len(result.Rows) > normalized.limit, compiledSchema.Columns[0].Datatype, nil
}

func normalizedDimensionValueKey(value any, dataType ossie.DataType) (string, bool) {
	switch dataType {
	case ossie.DataTypeString, ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		text, ok := value.(string)
		return "string:" + text, ok
	case ossie.DataTypeInteger:
		number, ok := value.(json.Number)
		if !ok {
			return "", false
		}
		integer, ok := new(big.Int).SetString(number.String(), 10)
		if !ok {
			return "", false
		}
		return "number:" + integer.String(), true
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		var text string
		if dataType == ossie.DataTypeDecimal {
			var ok bool
			text, ok = value.(string)
			if !ok {
				return "", false
			}
		} else {
			number, ok := value.(json.Number)
			if !ok {
				return "", false
			}
			text = number.String()
		}
		if strings.Contains(text, "/") {
			return "", false
		}
		number, ok := new(big.Rat).SetString(text)
		if !ok {
			return "", false
		}
		return "number:" + number.RatString(), true
	case ossie.DataTypeBoolean:
		boolean, ok := value.(bool)
		if !ok {
			return "", false
		}
		if boolean {
			return "boolean:true", true
		}
		return "boolean:false", true
	default:
		return "", false
	}
}

func supportedDimensionValuesDataType(dataType ossie.DataType) bool {
	switch dataType {
	case ossie.DataTypeString, ossie.DataTypeInteger, ossie.DataTypeDecimal, ossie.DataTypeFloat, ossie.DataTypeBoolean,
		ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}

func validNormalizedDimensionValue(value any, dataType ossie.DataType) bool {
	switch dataType {
	case ossie.DataTypeString, ossie.DataTypeDecimal, ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		_, ok := value.(string)
		return ok
	case ossie.DataTypeInteger, ossie.DataTypeFloat:
		_, ok := value.(json.Number)
		return ok
	case ossie.DataTypeBoolean:
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

func sameOutputSchema(left, right artifact.OutputSchema) bool {
	if len(left.Columns) != len(right.Columns) {
		return false
	}
	for index := range left.Columns {
		if left.Columns[index].Name != right.Columns[index].Name || left.Columns[index].Kind != right.Columns[index].Kind || left.Columns[index].Datatype != right.Columns[index].Datatype || !sameTimeGrain(left.Columns[index].Grain, right.Columns[index].Grain) {
			return false
		}
	}
	return true
}

func sameTimeGrain(left, right *query.TimeGrain) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneTimeGrain(grain *query.TimeGrain) *query.TimeGrain {
	if grain == nil {
		return nil
	}
	copy := *grain
	return &copy
}

func inconsistentDimensionValuesResult(reason string) error {
	return serrors.Internal("dimension value result violated its consistency contract", map[string]any{"reason": reason})
}
