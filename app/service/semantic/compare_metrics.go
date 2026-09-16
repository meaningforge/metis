package semantic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	analytics "github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/serrors"
)

const (
	maxComparisonMetrics    = 16
	maxComparisonDimensions = 8
)

type MetricComparisonQuery struct {
	ProjectID     string           `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Metrics       []string         `json:"metrics" jsonschema:"One through sixteen canonical governed metric refs."`
	TimeDimension string           `json:"time_dimension" jsonschema:"Canonical time-dimension ref compatible with every selected metric."`
	Baseline      analytics.Period `json:"baseline" jsonschema:"Exact baseline half-open period [start, end)."`
	Current       analytics.Period `json:"current" jsonschema:"Exact current half-open period [start, end)."`
	Dimensions    []string         `json:"dimensions,omitempty" jsonschema:"Up to eight canonical dimension refs forming one shared comparison grain."`
	Filters       []query.Filter   `json:"filters,omitempty" jsonschema:"Typed semantic predicates applied identically to both periods."`
}

type CompareMetricsService struct {
	compile               *CompileService
	discovery             *DiscoveryService
	projects              ExecutionProjectResolver
	runtime               *runner.Runner
	authorizer            ProjectAuthorizer
	authorizationObserver ProjectAuthorizationObserver
	observations          *observability.Recorder
}

func (s *CompareMetricsService) WithObservability(recorder *observability.Recorder) *CompareMetricsService {
	if s != nil {
		s.observations = recorder
	}
	return s
}

func NewCompareMetricsService(compile *CompileService, discovery *DiscoveryService, projects ExecutionProjectResolver, executionRuntime *runner.Runner) *CompareMetricsService {
	return &CompareMetricsService{
		compile: compile, discovery: discovery, projects: projects, runtime: executionRuntime,
		authorizer: ScopeProjectAuthorizer{},
	}
}

func (s *CompareMetricsService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *CompareMetricsService {
	if s != nil {
		s.authorizer = authorizer
	}
	return s
}

func (s *CompareMetricsService) WithProjectAuthorizationObserver(observer ProjectAuthorizationObserver) *CompareMetricsService {
	if s != nil {
		s.authorizationObserver = observer
	}
	return s
}

func (s *CompareMetricsService) CompareMetrics(ctx context.Context, input MetricComparisonQuery) (result *analytics.Result, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	var executedQueries int
	var observedRows, observedBytes int64
	var comparisonRecorder *observability.Recorder
	if s != nil {
		comparisonRecorder = s.observations
	}
	defer func() {
		observation := observability.ComparisonObservation{Result: observability.ResultSuccess, Duration: time.Since(started), Queries: executedQueries, Rows: observedRows, Bytes: observedBytes}
		if err != nil {
			observation.Result = observability.ResultError
			observation.Code = observability.ErrorCode(err)
		}
		comparisonRecorder.Record(ctx, observation)
	}()
	if s == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "metric comparison execution is not configured")
	}
	if s.projects == nil {
		if authErr := authorizeProject(ctx, s.authorizer, s.authorizationObserver, input.ProjectID, ProjectActionExecute); authErr != nil {
			return nil, authErr
		}
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "metric comparison execution is not configured")
	}
	project, err := s.projects.ResolveProject(input.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, project, ProjectActionExecute); err != nil {
		return nil, err
	}
	if s.compile == nil || s.discovery == nil || s.runtime == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "metric comparison execution is not configured")
	}
	input.ProjectID = project
	normalized, err := s.normalizeComparison(input)
	if err != nil {
		return nil, err
	}
	visibilityQuery := query.SemanticQuery{Project: normalized.Project, Model: normalized.Model, Filters: append([]query.Filter(nil), normalized.Filters...)}
	for _, metric := range normalized.Metrics {
		visibilityQuery.Metrics = append(visibilityQuery.Metrics, query.MetricRef{Name: metric.Internal})
	}
	for _, dimension := range normalized.Dimensions {
		visibilityQuery.Dimensions = append(visibilityQuery.Dimensions, query.DimensionRef{Name: dimension.Internal})
	}
	visibilityQuery.Dimensions = append(visibilityQuery.Dimensions, query.DimensionRef{Name: normalized.TimeDimension})
	if s.compile.discovery != nil {
		if err := s.compile.discovery.authorizeSemanticQueryAssets(ctx, visibilityQuery, ProjectActionExecute); err != nil {
			return nil, err
		}
	}
	dataSource, ok := dataSourceForModel(s.projects, project, normalized.Model)
	if !ok {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "semantic model has no configured DataSource")
	}
	route, err := s.runtime.ResolveDataSource(dataSource)
	if err != nil {
		return nil, mapExecutionError(err)
	}
	baseline, current, descriptor, err := s.compileComparison(ctx, normalized, route.Backend.Renderer)
	if err != nil {
		return nil, err
	}
	if err := analytics.ValidateEvidenceSchema(descriptor, baseline.OutputSchema); err != nil {
		var unsupported *analytics.UnsupportedEvidenceError
		if errors.As(err, &unsupported) {
			return nil, &serrors.Error{Code: serrors.ErrUnsupportedMetricComparison, Message: "metric comparison output datatype is unsupported", Details: map[string]any{"reason": "unsupported_evidence_datatype"}}
		}
		return nil, serrors.Internal("compiled comparison output schema is inconsistent", nil)
	}
	if !sameComparisonSchema(baseline.OutputSchema, current.OutputSchema) {
		return nil, serrors.Internal("compiled comparison period schemas diverged", nil)
	}
	analysisID, err := newQueryID()
	if err != nil {
		return nil, serrors.Internal("could not create analysis identifier", nil)
	}
	descriptor.AnalysisID = analysisID
	executionCtx := runner.WithQueryID(ctx, analysisID)
	timeout, err := time.ParseDuration(route.Source.Policy.QueryTimeout)
	if err != nil || route.Source.Policy.MaxRows == nil || route.Source.Policy.MaxBytes == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "DataSource execution policy is unavailable")
	}
	executionCtx, cancel := context.WithTimeout(executionCtx, timeout)
	defer cancel()
	remainingRows, remainingBytes := *route.Source.Policy.MaxRows, *route.Source.Policy.MaxBytes
	if remainingRows <= 0 || remainingBytes <= 0 {
		return nil, queryExecutionError(serrors.ErrQueryExecutionLimit, "metric comparison execution limit exceeded")
	}
	baselineResult, err := s.runtime.ExecuteResolved(executionCtx, route, baseline, runner.ExecutionOptions{MaxRows: remainingRows, MaxBytes: remainingBytes})
	if err != nil {
		return nil, mapExecutionError(err)
	}
	executedQueries++
	observedRows += baselineResult.Count
	observedBytes += baselineResult.Bytes
	remainingRows -= baselineResult.Count
	remainingBytes -= baselineResult.Bytes
	if remainingRows <= 0 || remainingBytes <= 0 {
		return nil, queryExecutionError(serrors.ErrQueryExecutionLimit, "metric comparison execution limit exceeded")
	}
	currentResult, err := s.runtime.ExecuteResolved(executionCtx, route, current, runner.ExecutionOptions{MaxRows: remainingRows, MaxBytes: remainingBytes})
	if err != nil {
		return nil, mapExecutionError(err)
	}
	executedQueries++
	observedRows += currentResult.Count
	observedBytes += currentResult.Bytes
	result, err = analytics.Evaluate(descriptor,
		analytics.Evidence{Schema: baselineResult.Schema, Rows: baselineResult.Rows},
		analytics.Evidence{Schema: currentResult.Schema, Rows: currentResult.Rows},
	)
	if err != nil {
		return nil, &serrors.Error{Code: serrors.ErrInconsistentComparisonResult, Message: "complete metric comparison evidence violated its consistency contract"}
	}
	return result, nil
}

type normalizedComparisonRef struct {
	Public   string
	Internal string
}

type normalizedComparisonQuery struct {
	Project, Model, TimeDimension string
	PublicTimeDimension           string
	Baseline, Current             comparisonTimeRange
	PublicBaseline, PublicCurrent analytics.Period
	Metrics, Dimensions           []normalizedComparisonRef
	Filters                       []query.Filter
}

type comparisonTimeRange struct{ Start, End time.Time }

func (s *CompareMetricsService) normalizeComparison(input MetricComparisonQuery) (normalizedComparisonQuery, error) {
	if len(input.Metrics) < 1 || len(input.Metrics) > maxComparisonMetrics {
		return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics requires from one through sixteen metrics"}
	}
	if len(input.Dimensions) > maxComparisonDimensions {
		return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics accepts at most eight dimensions"}
	}
	if s.discovery.manifest == nil {
		return normalizedComparisonQuery{}, serrors.Internal("semantic manifest is not configured", nil)
	}
	manifest := s.discovery.manifest.Current()
	if manifest == nil {
		return normalizedComparisonQuery{}, serrors.Internal("semantic manifest is not loaded", nil)
	}
	project, err := manifest.Project(input.ProjectID)
	if err != nil {
		return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found"}
	}
	if !strings.HasPrefix(input.TimeDimension, "dimension:") {
		return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics requires a canonical time-dimension ref"}
	}
	timeModel, timeDimension, timeHandle, err := resolveAgentDimensionRef(project, input.TimeDimension)
	if err != nil {
		return normalizedComparisonQuery{}, err
	}
	if !isAgentTimeDimension(timeHandle.Field) {
		return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension must be a governed time dimension"}
	}
	baseline, publicBaseline, err := parseComparisonPeriod("baseline", input.Baseline)
	if err != nil {
		return normalizedComparisonQuery{}, err
	}
	current, publicCurrent, err := parseComparisonPeriod("current", input.Current)
	if err != nil {
		return normalizedComparisonQuery{}, err
	}
	out := normalizedComparisonQuery{
		Project: input.ProjectID, Model: timeModel, TimeDimension: timeDimension, PublicTimeDimension: input.TimeDimension,
		Baseline: baseline, Current: current, PublicBaseline: publicBaseline, PublicCurrent: publicCurrent,
		Filters: append([]query.Filter(nil), input.Filters...),
	}
	seenMetrics := map[string]struct{}{}
	for _, raw := range input.Metrics {
		if !strings.HasPrefix(raw, "metric:") {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics requires canonical metric refs"}
		}
		model, metric, resolveErr := resolveAgentMetricRef(project, raw)
		if resolveErr != nil {
			return normalizedComparisonQuery{}, resolveErr
		}
		if model != timeModel {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "comparison refs must resolve to one model"}
		}
		if _, exists := seenMetrics[metric]; exists {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics metrics must be distinct"}
		}
		seenMetrics[metric] = struct{}{}
		out.Metrics = append(out.Metrics, normalizedComparisonRef{Public: raw, Internal: metric})
	}
	seenDimensions := map[string]struct{}{}
	for _, raw := range input.Dimensions {
		if !strings.HasPrefix(raw, "dimension:") {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics requires canonical dimension refs"}
		}
		model, dimension, handle, resolveErr := resolveAgentDimensionRef(project, raw)
		if resolveErr != nil {
			return normalizedComparisonQuery{}, resolveErr
		}
		if model != timeModel || isAgentTimeDimension(handle.Field) {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "comparison dimensions must be compatible non-time dimensions"}
		}
		if dimension == timeDimension || raw == input.TimeDimension {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension cannot be a comparison dimension"}
		}
		if _, exists := seenDimensions[dimension]; exists {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics dimensions must be distinct"}
		}
		seenDimensions[dimension] = struct{}{}
		out.Dimensions = append(out.Dimensions, normalizedComparisonRef{Public: raw, Internal: dimension})
	}
	for i := range out.Filters {
		if !strings.HasPrefix(out.Filters[i].Field, "metric:") && !strings.HasPrefix(out.Filters[i].Field, "dimension:") {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compare_metrics requires canonical filter field refs"}
		}
		if out.Filters[i].Field == input.TimeDimension {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension cannot be a shared comparison filter"}
		}
		field, normalizeErr := normalizeAgentFilterField(project, timeModel, out.Filters[i].Field)
		if normalizeErr != nil {
			return normalizedComparisonQuery{}, normalizeErr
		}
		if field == timeDimension {
			return normalizedComparisonQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension cannot be a shared comparison filter"}
		}
		out.Filters[i].Field = field
	}
	sort.Slice(out.Metrics, func(i, j int) bool { return out.Metrics[i].Public < out.Metrics[j].Public })
	sort.Slice(out.Dimensions, func(i, j int) bool { return out.Dimensions[i].Public < out.Dimensions[j].Public })
	return out, nil
}

func parseComparisonPeriod(name string, period analytics.Period) (comparisonTimeRange, analytics.Period, error) {
	parse := func(field, value string) (time.Time, error) {
		if !explicitRFC3339Offset.MatchString(value) {
			return time.Time{}, fmt.Errorf("%s.%s requires an explicit RFC3339 offset", name, field)
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s.%s is not RFC3339", name, field)
		}
		return parsed.UTC(), nil
	}
	start, err := parse("start", period.Start)
	if err != nil {
		return comparisonTimeRange{}, analytics.Period{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: err.Error()}
	}
	end, err := parse("end", period.End)
	if err != nil {
		return comparisonTimeRange{}, analytics.Period{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: err.Error()}
	}
	if !start.Before(end) {
		return comparisonTimeRange{}, analytics.Period{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: name + " must be a non-empty half-open period"}
	}
	canonical := func(value time.Time) string { return value.Format(time.RFC3339Nano) }
	return comparisonTimeRange{Start: start, End: end}, analytics.Period{Start: canonical(start), End: canonical(end)}, nil
}

func (s *CompareMetricsService) compileComparison(ctx context.Context, normalized normalizedComparisonQuery, selected renderer.Renderer) (*artifact.CompiledQuery, *artifact.CompiledQuery, analytics.Descriptor, error) {
	if selected == nil {
		return nil, nil, analytics.Descriptor{}, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "DataSource Renderer is unavailable")
	}
	build := func(period comparisonTimeRange) query.SemanticQuery {
		semanticQuery := query.SemanticQuery{Project: normalized.Project, Model: normalized.Model, Filters: append([]query.Filter(nil), normalized.Filters...)}
		for _, metric := range normalized.Metrics {
			semanticQuery.Metrics = append(semanticQuery.Metrics, query.MetricRef{Name: metric.Internal})
		}
		for _, dimension := range normalized.Dimensions {
			semanticQuery.Dimensions = append(semanticQuery.Dimensions, query.DimensionRef{Name: dimension.Internal})
		}
		semanticQuery.Filters = append(semanticQuery.Filters,
			query.Filter{Field: normalized.TimeDimension, Operator: query.FilterGTE, Value: period.Start.Format(time.RFC3339Nano)},
			query.Filter{Field: normalized.TimeDimension, Operator: query.FilterLT, Value: period.End.Format(time.RFC3339Nano)},
		)
		return semanticQuery
	}
	compiled, err := s.compile.compileQueriesWithRenderer(ctx, []query.SemanticQuery{build(normalized.Baseline), build(normalized.Current)}, selected)
	if err != nil {
		return nil, nil, analytics.Descriptor{}, err
	}
	baseline, current := compiled[0], compiled[1]
	descriptor := analytics.Descriptor{
		TimeDimension: normalized.PublicTimeDimension, Baseline: normalized.PublicBaseline, Current: normalized.PublicCurrent,
		Metrics: make([]analytics.OutputRef, len(normalized.Metrics)), Dimensions: make([]analytics.OutputRef, len(normalized.Dimensions)),
	}
	for i, metric := range normalized.Metrics {
		descriptor.Metrics[i] = analytics.OutputRef{Public: metric.Public, Column: metric.Internal}
	}
	for i, dimension := range normalized.Dimensions {
		descriptor.Dimensions[i] = analytics.OutputRef{Public: dimension.Public, Column: dimension.Internal}
	}
	return baseline, current, descriptor, nil
}

func sameComparisonSchema(left, right artifact.OutputSchema) bool {
	if len(left.Columns) != len(right.Columns) {
		return false
	}
	for i := range left.Columns {
		if left.Columns[i].Name != right.Columns[i].Name || left.Columns[i].Kind != right.Columns[i].Kind || left.Columns[i].Datatype != right.Columns[i].Datatype {
			return false
		}
	}
	return true
}
