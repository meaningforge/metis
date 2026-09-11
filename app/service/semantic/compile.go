package semantic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/app/service/policy"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

// CompileService orchestrates the semantic query use case across execution
// Renderer selection, semantic resolution/validation, planning, and physical
// compilation. It is transport-neutral.
type CompileService struct {
	dataAccessPolicy      policy.DataAccessPolicy
	resolver              *resolver.Resolver
	planner               *planner.Planner
	compiler              *compiler.Compiler
	projectResolver       ProjectResolver
	authorizer            ProjectAuthorizer
	authorizationObserver ProjectAuthorizationObserver
	discovery             *DiscoveryService
	extensionInventory    *extension.Inventory
	extensionCapabilities *extension.Registry
	observations          *observability.Recorder
	tracing               *observability.Tracing
	pipeline              *observability.Pipeline
}

// WithObservability attaches transport-neutral compile observation. A nil
// recorder or tracer remains a no-op and never changes compilation semantics.
func (s *CompileService) WithObservability(recorder *observability.Recorder, tracing *observability.Tracing) *CompileService {
	if s != nil {
		s.observations = recorder
		s.tracing = tracing
		s.pipeline = observability.NewPipeline(recorder, tracing)
		if s.planner != nil {
			s.planner.WithRuntimeObserver(s.pipeline)
		}
	}
	return s
}

func NewCompileService(resolver *resolver.Resolver, planner *planner.Planner, compiler *compiler.Compiler) *CompileService {
	inventory := extension.NewInventory()
	capabilities := extension.NewRegistry()
	if resolver != nil {
		resolver.WithExtensionCapabilities(inventory, capabilities)
	}
	return &CompileService{
		dataAccessPolicy:      policy.NoRestrictionDataAccessPolicy{},
		resolver:              resolver,
		planner:               planner,
		compiler:              compiler,
		authorizer:            ScopeProjectAuthorizer{},
		extensionInventory:    inventory,
		extensionCapabilities: capabilities,
	}
}

func (s *CompileService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *CompileService {
	if s != nil {
		s.authorizer = authorizer
	}
	return s
}

func (s *CompileService) WithProjectAuthorizationObserver(observer ProjectAuthorizationObserver) *CompileService {
	if s != nil {
		s.authorizationObserver = observer
	}
	return s
}

func (s *CompileService) WithProjectResolver(resolver ProjectResolver) *CompileService {
	if s != nil {
		s.projectResolver = resolver
	}
	return s
}

// WithDiscovery attaches the existing semantic discovery read model for
// bounded diagnostic candidate evidence. Validation semantics remain owned by
// Resolver/Planner; discovery suggestions never resolve or mutate a query.
func (s *CompileService) WithDiscovery(discovery *DiscoveryService) *CompileService {
	if s != nil {
		s.discovery = discovery
	}
	return s
}

type CompileRequest struct {
	Query                   query.SemanticQuery `json:"query"`
	Dialect                 sql.SQLDialect      `json:"dialect"`
	ProjectContextInherited bool                `json:"-"`
}

type DiagnosticSeverity string

const DiagnosticSeverityError DiagnosticSeverity = "error"

const (
	diagnosticCandidateLimit        = 5
	semanticListMetricsRepairTool   = "list_metrics"
	semanticGetDimensionsRepairTool = "get_dimensions"
)

type DiagnosticSubject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type SemanticSuggestion struct {
	Kind         string        `json:"kind"`
	Value        string        `json:"value"`
	MatchReasons []MatchReason `json:"match_reasons,omitempty"`
}

type SemanticRepairAction struct {
	Tool      string `json:"tool"`
	Arguments any    `json:"arguments"`
}

type SemanticDiagnostic struct {
	Code        serrors.ErrorCode     `json:"code"`
	Severity    DiagnosticSeverity    `json:"severity"`
	Subject     *DiagnosticSubject    `json:"subject,omitempty"`
	Message     string                `json:"message,omitempty"`
	Details     map[string]any        `json:"details,omitempty"`
	Suggestions []SemanticSuggestion  `json:"suggestions,omitempty"`
	Repair      *SemanticRepairAction `json:"repair,omitempty"`
}

type QueryValidationResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []SemanticDiagnostic `json:"diagnostics,omitempty"`
}

func (s *CompileService) Validate(ctx context.Context, req CompileRequest) (*QueryValidationResult, error) {
	if _, _, err := s.prepareSemanticQuery(ctx, req); err != nil {
		var semanticErr *serrors.Error
		if !errors.As(err, &semanticErr) {
			return nil, err
		}
		if semanticErr.Code == serrors.ErrProjectAccessDenied || semanticErr.Code == serrors.ErrUnauthenticated {
			return nil, err
		}
		diagnostic := diagnosticFromSemanticError(semanticErr)
		s.enrichDiagnostic(ctx, req, &diagnostic)
		return &QueryValidationResult{Diagnostics: []SemanticDiagnostic{diagnostic}}, nil
	}
	return &QueryValidationResult{Valid: true}, nil
}

func diagnosticFromSemanticError(err *serrors.Error) SemanticDiagnostic {
	diagnostic := SemanticDiagnostic{
		Code:     err.Code,
		Severity: DiagnosticSeverityError,
		Message:  err.Message,
		Details:  err.Details,
	}
	for _, key := range []string{"metric", "dimension", "field"} {
		if value, ok := err.Details[key].(string); ok && value != "" {
			diagnostic.Subject = &DiagnosticSubject{Kind: key, Name: value}
			break
		}
	}
	for _, suggestion := range err.Suggestions {
		diagnostic.Suggestions = append(diagnostic.Suggestions, SemanticSuggestion{Kind: "hint", Value: suggestion})
	}
	return diagnostic
}

func diagnosticRepairKind(code serrors.ErrorCode) (AssetKind, string, bool) {
	switch code {
	case serrors.ErrMetricNotFound:
		return AssetMetric, "metric_candidate", true
	case serrors.ErrDimensionNotFound, serrors.ErrFieldNotFound, serrors.ErrAmbiguousField:
		return AssetDimension, "dimension_candidate", true
	default:
		return "", "", false
	}
}

func repairActionForDiagnostic(req CompileRequest, diagnostic *SemanticDiagnostic) *SemanticRepairAction {
	if diagnostic == nil || diagnostic.Subject == nil {
		return nil
	}
	switch diagnostic.Code {
	case serrors.ErrMetricNotFound:
		projectID := req.Query.Project
		if req.ProjectContextInherited {
			projectID = ""
		}
		return &SemanticRepairAction{
			Tool: semanticListMetricsRepairTool,
			Arguments: ListMetricsRequest{
				ProjectID: projectID,
				Search:    []string{diagnostic.Subject.Name},
			},
		}
	case serrors.ErrDimensionNotFound, serrors.ErrFieldNotFound, serrors.ErrAmbiguousField:
		if req.Query.Model == "" {
			return nil
		}
		projectID := req.Query.Project
		if req.ProjectContextInherited {
			projectID = ""
		}
		arguments := GetDimensionsRequest{ProjectID: projectID, Search: []string{diagnostic.Subject.Name}}
		if len(req.Query.Metrics) == 0 {
			arguments.Model = "model:" + req.Query.Model
		} else {
			arguments.Metrics = make([]string, 0, len(req.Query.Metrics))
			for _, metric := range req.Query.Metrics {
				arguments.Metrics = append(arguments.Metrics, "metric:"+req.Query.Model+"."+metric.Name)
			}
		}
		return &SemanticRepairAction{Tool: semanticGetDimensionsRepairTool, Arguments: arguments}
	default:
		return nil
	}
}

func (s *CompileService) enrichDiagnostic(ctx context.Context, req CompileRequest, diagnostic *SemanticDiagnostic) {
	if diagnostic == nil {
		return
	}
	diagnostic.Repair = repairActionForDiagnostic(req, diagnostic)
	if s == nil || s.discovery == nil || diagnostic.Subject == nil {
		return
	}

	kind, suggestionKind, ok := diagnosticRepairKind(diagnostic.Code)
	if !ok {
		return
	}
	result, err := s.discovery.SearchSemantics(ctx, SearchSemanticsRequest{
		Project: req.Query.Project,
		Model:   req.Query.Model,
		Query:   diagnostic.Subject.Name,
		Kinds:   []AssetKind{kind},
		Limit:   diagnosticCandidateLimit,
	})
	if err != nil || result == nil {
		return
	}

	seen := make(map[string]struct{}, len(diagnostic.Suggestions)+len(result.Matches))
	for _, suggestion := range diagnostic.Suggestions {
		seen[suggestion.Kind+"\x00"+suggestion.Value] = struct{}{}
	}
	for _, match := range result.Matches {
		value := match.Qualified
		if value == "" {
			value = match.Name
		}
		key := suggestionKind + "\x00" + value
		if value == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		diagnostic.Suggestions = append(diagnostic.Suggestions, SemanticSuggestion{
			Kind:         suggestionKind,
			Value:        value,
			MatchReasons: append([]MatchReason(nil), match.MatchReasons...),
		})
	}
}

func (s *CompileService) enrichCompileError(_ context.Context, req CompileRequest, err error) error {
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) {
		return err
	}
	diagnostic := diagnosticFromSemanticError(semanticErr)
	repair := repairActionForDiagnostic(req, &diagnostic)
	if repair == nil {
		return err
	}
	details := make(map[string]any, len(semanticErr.Details)+1)
	for key, value := range semanticErr.Details {
		details[key] = value
	}
	details["repair"] = repair
	return &serrors.Error{
		Code:        semanticErr.Code,
		Message:     semanticErr.Message,
		Details:     details,
		Suggestions: append([]string(nil), semanticErr.Suggestions...),
	}
}

func (s *CompileService) Compile(ctx context.Context, req CompileRequest) (compiled *artifact.CompiledQuery, err error) {
	project, resolveErr := s.resolveAuthorizedProject(ctx, req.Query.Project, ProjectActionCompile)
	if resolveErr != nil {
		return nil, resolveErr
	}
	req.Query.Project = project
	if s.discovery != nil {
		if err := s.discovery.authorizeSemanticQueryAssets(ctx, req.Query, ProjectActionCompile); err != nil {
			return nil, err
		}
	}
	renderer, err := s.compiler.ResolveRenderer(req.Dialect)
	if err != nil {
		return nil, err
	}
	return s.compileWithRenderer(ctx, req, renderer)
}

// compileWithRenderer compiles a query using the exact Renderer selected by
// the caller. Runtime execution uses this path after DataSource -> Backend
// resolution, which prevents a second renderer authority lookup.
func (s *CompileService) compileWithRenderer(ctx context.Context, req CompileRequest, renderer renderer.Renderer) (*artifact.CompiledQuery, error) {
	return s.observeCompilation(ctx, renderer, func(ctx context.Context, dialect observability.Dialect) (*artifact.CompiledQuery, error) {
		plan, err := s.prepareSemanticQueryWithRenderer(ctx, req.Query, renderer)
		if err != nil {
			return nil, s.enrichCompileError(ctx, req, err)
		}
		return observeCompilePhase(s.pipeline, ctx, observability.PhaseRendering, dialect, func(renderingCtx context.Context) (*artifact.CompiledQuery, error) {
			return s.compilePreparedPlan(renderingCtx, req.Query, plan, renderer)
		})
	})
}

func (s *CompileService) observeCompilation(ctx context.Context, renderer renderer.Renderer, compile func(context.Context, observability.Dialect) (*artifact.CompiledQuery, error)) (compiled *artifact.CompiledQuery, err error) {
	started := time.Now()
	ctx, span := s.tracing.Start(ctx, observability.OperationCompile)
	ctx = s.pipeline.Activate(ctx)
	dialect := observability.DialectUnresolved
	completed := false
	defer func() {
		observationErr := err
		if observationErr == nil && !completed {
			observationErr = errors.New("compile terminated before completion")
		}
		observation := observability.CompileObservation{
			Dialect: dialect, Result: observability.ResultSuccess, Duration: time.Since(started),
		}
		if observationErr != nil {
			observation.Result = observability.ResultError
			observation.Code = observability.ErrorCode(observationErr)
			s.tracing.RecordError(ctx, observationErr, observation.Code)
		}
		s.observations.Record(ctx, observation)
		span.End()
	}()

	if renderer != nil {
		dialect = observability.NormalizeDialect(string(renderer.SQLDialect()))
	}
	compiled, err = compile(ctx, dialect)
	completed = true
	return compiled, err
}

// prepareSemanticQuery is the single orchestration boundary shared by compile,
// validation, and Agent-facing semantic explanation. It intentionally stops
// before physical compilation so these operations cannot grow independent
// semantic resolution/planning paths.
func (s *CompileService) prepareSemanticQuery(ctx context.Context, req CompileRequest) (*semanticplan.SemanticPlan, renderer.Renderer, error) {
	project, resolveErr := s.resolveAuthorizedProject(ctx, req.Query.Project, ProjectActionCompile)
	if resolveErr != nil {
		return nil, nil, resolveErr
	}
	req.Query.Project = project
	if s.discovery != nil {
		if err := s.discovery.authorizeSemanticQueryAssets(ctx, req.Query, ProjectActionCompile); err != nil {
			return nil, nil, err
		}
	}
	renderer, err := s.compiler.ResolveRenderer(req.Dialect)
	if err != nil {
		return nil, renderer, err
	}
	plan, err := s.prepareSemanticQueryWithRenderer(ctx, req.Query, renderer)
	return plan, renderer, err
}

func (s *CompileService) resolveAuthorizedProject(ctx context.Context, project string, action ProjectAction) (string, error) {
	project = strings.TrimSpace(project)
	if s != nil && s.projectResolver != nil {
		resolved, err := s.projectResolver.ResolveProject(project)
		if err != nil {
			return "", err
		}
		project = resolved
	}
	if project == "" {
		return "", &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, project, action); err != nil {
		return "", err
	}
	return project, nil
}

// prepareSemanticQueryWithRenderer is the renderer-authoritative semantic
// pipeline. The query project must already be resolved by its caller.
func (s *CompileService) prepareSemanticQueryWithRenderer(ctx context.Context, semanticQuery query.SemanticQuery, renderer renderer.Renderer) (*semanticplan.SemanticPlan, error) {
	plans, err := s.prepareQueriesWithRenderer(ctx, []query.SemanticQuery{semanticQuery}, renderer)
	if err != nil {
		return nil, err
	}
	return plans[0], nil
}

func (s *CompileService) resolveQueryWithRenderer(ctx context.Context, semanticQuery query.SemanticQuery, renderer renderer.Renderer) (*resolver.SemanticQuerySpec, error) {
	if renderer == nil {
		return nil, fmt.Errorf("Renderer is required")
	}
	dialect := observability.NormalizeDialect(string(renderer.SQLDialect()))
	resolved, err := observeCompilePhase(s.pipeline, ctx, observability.PhaseAnalysis, dialect, func(analysisCtx context.Context) (*resolver.SemanticQuerySpec, error) {
		resolved, resolveErr := s.resolver.ResolveForRenderer(analysisCtx, semanticQuery, renderer)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if capabilityErr := s.validateExtensionCapabilities(resolved.Model.Model, renderer); capabilityErr != nil {
			return nil, capabilityErr
		}
		return resolved, nil
	})
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

func observeCompilePhase[T any](pipeline *observability.Pipeline, ctx context.Context, phase observability.Phase, dialect observability.Dialect, run func(context.Context) (T, error)) (result T, err error) {
	observed, finish := pipeline.StartPhase(ctx, phase, dialect)
	completed := false
	defer func() {
		observationErr := err
		if observationErr == nil && !completed {
			observationErr = fmt.Errorf("%s terminated before completion", phase)
		}
		finish(observationErr)
	}()
	result, err = run(observed)
	completed = true
	return result, err
}
