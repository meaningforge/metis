package semantic

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	analytics "github.com/meaningforge/metis/analytics/attribution"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/serrors"
)

type MetricAttributionQuery struct {
	ProjectID     string                      `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Metric        string                      `json:"metric" jsonschema:"One canonical governed metric ref returned by list_metrics."`
	TimeDimension string                      `json:"time_dimension" jsonschema:"Canonical time-dimension ref compatible with the selected metric."`
	Baseline      analytics.AttributionPeriod `json:"baseline" jsonschema:"Exact baseline half-open period [start, end)."`
	Current       analytics.AttributionPeriod `json:"current" jsonschema:"Exact current half-open period [start, end)."`
	Dimensions    []string                    `json:"dimensions" jsonschema:"One through sixteen canonical dimension refs evaluated independently; do not infer cross-dimension ranking."`
	Filters       []query.Filter              `json:"filters,omitempty" jsonschema:"Typed semantic predicates applied identically to both periods."`
}

type AttributeMetricService struct {
	compile               *CompileService
	discovery             *DiscoveryService
	projects              ExecutionProjectResolver
	runtime               *runner.Runner
	authorizer            ProjectAuthorizer
	authorizationObserver ProjectAuthorizationObserver
	observations          *observability.Recorder
}

func (s *AttributeMetricService) WithObservability(recorder *observability.Recorder) *AttributeMetricService {
	if s != nil {
		s.observations = recorder
	}
	return s
}

func NewAttributeMetricService(compile *CompileService, discovery *DiscoveryService, projects ExecutionProjectResolver, executionRuntime *runner.Runner) *AttributeMetricService {
	return &AttributeMetricService{
		compile: compile, discovery: discovery, projects: projects, runtime: executionRuntime,
		authorizer: ScopeProjectAuthorizer{},
	}
}

func (s *AttributeMetricService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *AttributeMetricService {
	if s != nil {
		s.authorizer = authorizer
	}
	return s
}

func (s *AttributeMetricService) WithProjectAuthorizationObserver(observer ProjectAuthorizationObserver) *AttributeMetricService {
	if s != nil {
		s.authorizationObserver = observer
	}
	return s
}

func (s *AttributeMetricService) AttributeMetric(ctx context.Context, input MetricAttributionQuery) (result *analytics.AttributeMetricResult, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	var executedQueries int
	var observedRows, observedBytes int64
	var attributionRecorder *observability.Recorder
	if s != nil {
		attributionRecorder = s.observations
	}
	defer func() {
		observation := observability.AttributionObservation{Result: observability.ResultSuccess, Duration: time.Since(started), Queries: executedQueries, Rows: observedRows, Bytes: observedBytes}
		if err != nil {
			observation.Result = observability.ResultError
			observation.Code = observability.ErrorCode(err)
		}
		attributionRecorder.Record(ctx, observation)
	}()
	if s == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "metric attribution execution is not configured")
	}
	if s.projects == nil {
		if authErr := authorizeProject(ctx, s.authorizer, s.authorizationObserver, input.ProjectID, ProjectActionExecute); authErr != nil {
			return nil, authErr
		}
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "metric attribution execution is not configured")
	}
	project, err := s.projects.ResolveProject(input.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, project, ProjectActionExecute); err != nil {
		return nil, err
	}
	if s.compile == nil || s.discovery == nil || s.runtime == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "metric attribution execution is not configured")
	}
	input.ProjectID = project
	normalized, err := s.normalize(input)
	if err != nil {
		return nil, err
	}
	visibilityQuery := query.SemanticQuery{Project: normalized.Project, Model: normalized.Model, Metrics: []query.MetricRef{{Name: normalized.Metric}}, Filters: append([]query.Filter(nil), normalized.Filters...)}
	visibilityQuery.Dimensions = append(visibilityQuery.Dimensions, query.DimensionRef{Name: normalized.TimeDimension})
	for _, dimension := range normalized.Dimensions {
		visibilityQuery.Dimensions = append(visibilityQuery.Dimensions, query.DimensionRef{Name: dimension.Internal})
	}
	if s.compile.discovery != nil {
		if err := s.compile.discovery.authorizeSemanticQueryAssets(ctx, visibilityQuery, ProjectActionExecute); err != nil {
			return nil, err
		}
	}
	dataSource, ok := s.projects.DataSourceForProject(project)
	if !ok {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "project has no configured DataSource")
	}
	route, err := s.runtime.ResolveDataSource(dataSource)
	if err != nil {
		return nil, mapExecutionError(err)
	}
	compilation, strategy, dimensionRefs, err := s.compileBundle(ctx, normalized, route.Backend.Renderer)
	if err != nil {
		return nil, err
	}
	for _, item := range compilation.Queries {
		if schemaErr := analytics.ValidateEvidenceSchema(strategy, dimensionRefs[item.Dimension], item.Dimension, item.OutputSchema); schemaErr != nil {
			var unsupported *analytics.UnsupportedEvidenceError
			if errors.As(schemaErr, &unsupported) {
				return nil, &serrors.Error{Code: serrors.ErrUnsupportedMetricAttribution, Message: "metric attribution output datatype is unsupported", Details: map[string]any{"metric": normalized.PublicMetric, "reason": "unsupported_evidence_datatype"}}
			}
			return nil, serrors.Internal("compiled attribution output schema is inconsistent", nil)
		}
	}
	analysisID, err := newQueryID()
	if err != nil {
		return nil, serrors.Internal("could not create analysis identifier", nil)
	}
	executionCtx := runner.WithQueryID(ctx, analysisID)
	timeout, err := time.ParseDuration(route.Source.Policy.QueryTimeout)
	if err != nil || route.Source.Policy.MaxRows == nil || route.Source.Policy.MaxBytes == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "DataSource execution policy is unavailable")
	}
	executionCtx, cancel := context.WithTimeout(executionCtx, timeout)
	defer cancel()
	remainingRows, remainingBytes := *route.Source.Policy.MaxRows, *route.Source.Policy.MaxBytes
	evidence := make([]analytics.DimensionEvidence, 0, len(compilation.Queries))
	for index, item := range compilation.Queries {
		if remainingRows <= 0 || remainingBytes <= 0 {
			return nil, queryExecutionError(serrors.ErrQueryExecutionLimit, "metric attribution bundle execution limit exceeded")
		}
		result, executeErr := s.runtime.ExecuteResolved(executionCtx, route, item.CompiledQuery, runner.ExecutionOptions{MaxRows: remainingRows, MaxBytes: remainingBytes})
		if executeErr != nil {
			return nil, mapExecutionError(executeErr)
		}
		executedQueries++
		observedRows += result.Count
		observedBytes += result.Bytes
		remainingRows -= result.Count
		remainingBytes -= result.Bytes
		publicDimension := dimensionRefs[item.Dimension]
		if publicDimension == "" {
			return nil, serrors.Internal("compiled attribution dimension lost its public identity", nil)
		}
		evidence = append(evidence, analytics.DimensionEvidence{Dimension: publicDimension, Column: item.Dimension, Schema: result.Schema, Rows: result.Rows})
		if index+1 < len(compilation.Queries) && (remainingRows == 0 || remainingBytes == 0) {
			return nil, queryExecutionError(serrors.ErrQueryExecutionLimit, "metric attribution bundle execution limit exceeded")
		}
	}
	evaluated, err := analytics.Evaluate(analytics.Descriptor{AnalysisID: analysisID, Metric: input.Metric, TimeDimension: input.TimeDimension, Baseline: normalized.PublicBaseline, Current: normalized.PublicCurrent, Strategy: strategy}, evidence)
	if err != nil {
		return nil, &serrors.Error{Code: serrors.ErrInconsistentAttributionResult, Message: "complete attribution evidence violated its reconciliation contract"}
	}
	return evaluated, nil
}

type normalizedAttributionQuery struct {
	Project, Model, Metric, TimeDimension string
	PublicMetric, PublicTimeDimension     string
	Baseline, Current                     semanticplan.MetricAttributionTimeRange
	PublicBaseline, PublicCurrent         analytics.AttributionPeriod
	Dimensions                            []normalizedAttributionDimension
	Filters                               []query.Filter
}

type normalizedAttributionDimension struct{ Public, Internal string }

var explicitRFC3339Offset = regexp.MustCompile(`(?:Z|[+-][0-9]{2}:[0-9]{2})$`)

func (s *AttributeMetricService) normalize(input MetricAttributionQuery) (normalizedAttributionQuery, error) {
	if len(input.Dimensions) == 0 || len(input.Dimensions) > attribution.MaxMetricAttributionDimensions {
		return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "attribute_metric requires from one through sixteen dimensions"}
	}
	if s.discovery.manifest == nil {
		return normalizedAttributionQuery{}, serrors.Internal("semantic manifest is not configured", nil)
	}
	manifest := s.discovery.manifest.Current()
	if manifest == nil {
		return normalizedAttributionQuery{}, serrors.Internal("semantic manifest is not loaded", nil)
	}
	project, err := manifest.Project(input.ProjectID)
	if err != nil {
		return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found"}
	}
	if !strings.HasPrefix(input.Metric, "metric:") || !strings.HasPrefix(input.TimeDimension, "dimension:") {
		return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "attribute_metric requires canonical metric and time-dimension refs"}
	}
	model, metric, err := resolveAgentMetricRef(project, input.Metric)
	if err != nil {
		return normalizedAttributionQuery{}, err
	}
	timeModel, timeDimension, timeHandle, err := resolveAgentDimensionRef(project, input.TimeDimension)
	if err != nil {
		return normalizedAttributionQuery{}, err
	}
	if timeModel != model || !isAgentTimeDimension(timeHandle.Field) {
		return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension must be a compatible time dimension in the metric model"}
	}
	baseline, publicBaseline, err := parseAttributionPeriod("baseline", input.Baseline)
	if err != nil {
		return normalizedAttributionQuery{}, err
	}
	current, publicCurrent, err := parseAttributionPeriod("current", input.Current)
	if err != nil {
		return normalizedAttributionQuery{}, err
	}
	out := normalizedAttributionQuery{Project: input.ProjectID, Model: model, Metric: metric, TimeDimension: timeDimension, PublicMetric: input.Metric, PublicTimeDimension: input.TimeDimension, Baseline: baseline, Current: current, PublicBaseline: publicBaseline, PublicCurrent: publicCurrent, Filters: append([]query.Filter(nil), input.Filters...)}
	seen := map[string]struct{}{}
	for _, raw := range input.Dimensions {
		if !strings.HasPrefix(raw, "dimension:") {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "attribute_metric requires canonical dimension refs"}
		}
		dimensionModel, dimension, handle, resolveErr := resolveAgentDimensionRef(project, raw)
		if resolveErr != nil {
			return normalizedAttributionQuery{}, resolveErr
		}
		if dimensionModel != model || isAgentTimeDimension(handle.Field) {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "decomposition dimensions must be compatible non-time dimensions"}
		}
		if dimension == timeDimension || raw == input.TimeDimension {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension cannot be a decomposition dimension"}
		}
		if _, exists := seen[dimension]; exists {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "attribute_metric dimensions must be distinct"}
		}
		seen[dimension] = struct{}{}
		out.Dimensions = append(out.Dimensions, normalizedAttributionDimension{Public: raw, Internal: dimension})
	}
	for i := range out.Filters {
		if !strings.HasPrefix(out.Filters[i].Field, "metric:") && !strings.HasPrefix(out.Filters[i].Field, "dimension:") {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "attribute_metric requires canonical filter field refs"}
		}
		if out.Filters[i].Field == input.TimeDimension {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension cannot be used as a shared attribution filter"}
		}
		field, normalizeErr := normalizeAgentFilterField(project, model, out.Filters[i].Field)
		if normalizeErr != nil {
			return normalizedAttributionQuery{}, normalizeErr
		}
		if field == timeDimension {
			return normalizedAttributionQuery{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "time_dimension cannot be used as a shared attribution filter"}
		}
		out.Filters[i].Field = field
	}
	sort.Slice(out.Dimensions, func(i, j int) bool { return out.Dimensions[i].Internal < out.Dimensions[j].Internal })
	return out, nil
}

func parseAttributionPeriod(name string, period analytics.AttributionPeriod) (semanticplan.MetricAttributionTimeRange, analytics.AttributionPeriod, error) {
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
		return semanticplan.MetricAttributionTimeRange{}, analytics.AttributionPeriod{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: err.Error()}
	}
	end, err := parse("end", period.End)
	if err != nil {
		return semanticplan.MetricAttributionTimeRange{}, analytics.AttributionPeriod{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: err.Error()}
	}
	if !start.Before(end) {
		return semanticplan.MetricAttributionTimeRange{}, analytics.AttributionPeriod{}, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: name + " must be a non-empty half-open period"}
	}
	canonical := func(value time.Time) string { return value.Format(time.RFC3339Nano) }
	return semanticplan.MetricAttributionTimeRange{Start: start, End: end}, analytics.AttributionPeriod{Start: canonical(start), End: canonical(end)}, nil
}

func (s *AttributeMetricService) compileBundle(ctx context.Context, normalized normalizedAttributionQuery, selected renderer.Renderer) (*compiler.MetricAttributionCompilation, analytics.AttributionStrategy, map[string]string, error) {
	if selected == nil {
		return nil, "", nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "DataSource Renderer is unavailable")
	}
	type plannedDimension struct {
		dimension normalizedAttributionDimension
		ordinary  *semanticplan.SemanticPlan
		proof     attribution.MetricAttributionPlan
	}
	planned := make([]plannedDimension, 0, len(normalized.Dimensions))
	var sharedProof attribution.MetricAttributionPlan
	var resolvedFilters []semanticplan.Predicate
	semanticQueries := make([]query.SemanticQuery, 0, len(normalized.Dimensions))
	for _, dimension := range normalized.Dimensions {
		semanticQuery := query.SemanticQuery{Project: normalized.Project, Model: normalized.Model, Metrics: []query.MetricRef{{Name: normalized.Metric}}, Dimensions: []query.DimensionRef{{Name: dimension.Internal}, {Name: normalized.TimeDimension}}, Filters: append([]query.Filter(nil), normalized.Filters...)}
		semanticQueries = append(semanticQueries, semanticQuery)
	}
	work, err := s.compile.prepareQueryWork(ctx, semanticQueries, selected)
	if err != nil {
		return nil, "", nil, err
	}
	for i, dimension := range normalized.Dimensions {
		resolved, evaluationPlan := work[i].Query, work[i].Evaluation
		proof, err := attribution.BuildMetricAttributionPlan(evaluationPlan, resolved.Model, normalized.Metric)
		if err != nil {
			return nil, "", nil, serrors.Internal("metric attribution proof construction failed", nil)
		}
		if proof.Exactness != attribution.MetricAttributionExact {
			return nil, "", nil, &serrors.Error{Code: serrors.ErrUnsupportedMetricAttribution, Message: "metric does not satisfy the governed attribution contract", Details: map[string]any{"metric": normalized.PublicMetric, "reason": proof.Reason}}
		}
		if len(planned) == 0 {
			sharedProof = proof
		} else if !reflect.DeepEqual(sharedProof, proof) {
			return nil, "", nil, serrors.Internal("attribution dimensions produced divergent decomposition proof", nil)
		}
		planned = append(planned, plannedDimension{dimension: dimension, proof: proof})
	}
	plans, err := s.compile.planQueryWork(ctx, work, selected)
	if err != nil {
		return nil, "", nil, err
	}
	for i := range planned {
		planned[i].ordinary = plans[i]
	}
	if len(plans) != 0 {
		resolvedFilters = append([]semanticplan.Predicate(nil), plans[0].Predicates...)
	}
	internalDimensions := make([]string, len(planned))
	refs := make(map[string]string, len(planned))
	for i, item := range planned {
		internalDimensions[i] = item.dimension.Internal
		refs[item.dimension.Internal] = item.dimension.Public
	}
	request := attribution.ResolvedMetricAttributionRequest{ProjectID: normalized.Project, MetricRef: normalized.Metric, TimeDimensionRef: normalized.TimeDimension, Baseline: normalized.Baseline, Current: normalized.Current, Dimensions: internalDimensions, Filters: resolvedFilters}
	queries := make([]attribution.MetricAttributionDimensionPlan, 0, len(planned))
	for _, item := range planned {
		plan, err := attribution.BuildDimensionPlan(request, item.proof, item.dimension.Internal, item.ordinary)
		if err != nil {
			return nil, "", nil, &serrors.Error{Code: serrors.ErrUnsupportedMetricAttribution, Message: "metric attribution plan is unsupported", Details: map[string]any{"metric": normalized.PublicMetric, "reason": "unsupported_source_shape"}}
		}
		queries = append(queries, attribution.MetricAttributionDimensionPlan{Dimension: item.dimension.Internal, Plan: plan})
	}
	bundle, err := attribution.BuildMetricAttributionBundle(request, queries)
	if err != nil {
		return nil, "", nil, serrors.Internal("metric attribution bundle validation failed", nil)
	}
	compiled, err := s.compile.compiler.CompileMetricAttributionBundleResolved(ctx, bundle, selected)
	if err != nil {
		if len(plans) > 0 && plans[0].PolicyScope != "" {
			return nil, "", nil, dataPolicyError(err)
		}
		return nil, "", nil, err
	}
	strategy := analytics.AttributionStrategy(sharedProof.Strategy)
	return compiled, strategy, refs, nil
}
