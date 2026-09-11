package semantic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

// ExecutionProjectResolver resolves the semantic namespace and exposes its
// direct deployment DataSource wiring. It must not contain target aliases or
// runtime overrides.
type ExecutionProjectResolver interface {
	ProjectResolver
	DataSourceForProject(project string) (string, bool)
}

// QueryMetricsRequest contains semantic intent only. Physical SQL, dialect,
// renderer, DataSource, credentials, and runtime options are deliberately not
// part of the Agent-facing execution contract.
type QueryMetricsRequest struct {
	Query                   query.SemanticQuery `json:"query"`
	ProjectContextInherited bool                `json:"-"`
}

// QueryMetricsResult is a bounded normalized execution result. It never
// exposes physical SQL, DataSource identity, credentials, or byte accounting.
type QueryMetricsResult struct {
	QueryID  string                `json:"query_id"`
	Schema   artifact.OutputSchema `json:"schema"`
	Rows     [][]any               `json:"rows"`
	Count    int64                 `json:"count"`
	Warnings []artifact.Warning    `json:"warnings,omitempty"`
}

// QueryMetricsService composes the only generic public semantic execution workflow:
// resolve project -> direct DataSource -> Backend -> same Renderer compile ->
// atomic execution. It remains transport-neutral.
type QueryMetricsService struct {
	compile               *CompileService
	projects              ExecutionProjectResolver
	runtime               *runner.Runner
	authorizer            ProjectAuthorizer
	authorizationObserver ProjectAuthorizationObserver
}

func NewQueryMetricsService(compile *CompileService, projects ExecutionProjectResolver, executionRuntime *runner.Runner) *QueryMetricsService {
	return &QueryMetricsService{
		compile:    compile,
		projects:   projects,
		runtime:    executionRuntime,
		authorizer: ScopeProjectAuthorizer{},
	}
}

func (s *QueryMetricsService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *QueryMetricsService {
	if s != nil {
		s.authorizer = authorizer
	}
	return s
}

func (s *QueryMetricsService) WithProjectAuthorizationObserver(observer ProjectAuthorizationObserver) *QueryMetricsService {
	if s != nil {
		s.authorizationObserver = observer
	}
	return s
}

// ProjectCapabilities reports the closed Agent operations wired for one
// Project. Resolution is read-only and never opens a Driver or resolves a
// secret. AgentSemanticService intersects this deployment capability with the
// caller's project/action decision exactly once.
func (s *QueryMetricsService) ProjectCapabilities(_ context.Context, project string) []AgentProjectCapability {
	if s == nil || s.compile == nil || s.projects == nil || s.runtime == nil {
		return nil
	}
	resolved, err := s.projects.ResolveProject(project)
	if err != nil || resolved != project {
		return nil
	}
	dataSource, ok := s.projects.DataSourceForProject(resolved)
	if !ok {
		return nil
	}
	if _, err := s.runtime.ResolveDataSource(dataSource); err == nil {
		return []AgentProjectCapability{AgentCapabilityQueryMetrics}
	}
	return nil
}

func (s *QueryMetricsService) QueryMetrics(ctx context.Context, req QueryMetricsRequest) (*QueryMetricsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "query execution is not configured")
	}
	if s.projects == nil {
		if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, req.Query.Project, ProjectActionExecute); err != nil {
			return nil, err
		}
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "query execution is not configured")
	}
	project, err := s.projects.ResolveProject(req.Query.Project)
	if err != nil {
		return nil, err
	}
	if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, project, ProjectActionExecute); err != nil {
		return nil, err
	}
	if s.compile == nil || s.runtime == nil {
		return nil, queryExecutionError(serrors.ErrQueryExecutionUnavailable, "query execution is not configured")
	}
	if len(req.Query.Metrics) == 0 {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "query_metrics requires at least one metric"}
	}
	queryID, err := newQueryID()
	if err != nil {
		return nil, serrors.Internal("could not create query identifier", nil)
	}
	ctx = runner.WithQueryID(ctx, queryID)
	req.Query.Project = project
	if s.compile != nil && s.compile.discovery != nil {
		if err := s.compile.discovery.authorizeSemanticQueryAssets(ctx, req.Query, ProjectActionExecute); err != nil {
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

	compiled, err := s.compile.compileWithRenderer(ctx, CompileRequest{
		Query:                   req.Query,
		ProjectContextInherited: req.ProjectContextInherited,
	}, route.Backend.Renderer)
	if err != nil {
		return nil, err
	}
	result, err := s.runtime.ExecuteResolved(ctx, route, compiled, runner.ExecutionOptions{})
	if err != nil {
		return nil, mapExecutionError(err)
	}
	return &QueryMetricsResult{
		QueryID:  queryID,
		Schema:   result.Schema,
		Rows:     result.Rows,
		Count:    result.Count,
		Warnings: append([]artifact.Warning(nil), compiled.Warnings...),
	}, nil
}

func newQueryID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func queryExecutionError(code serrors.ErrorCode, message string) *serrors.Error {
	return &serrors.Error{Code: code, Message: message}
}

func mapExecutionError(err error) error {
	var executionErr *runner.ExecutionError
	if !errors.As(err, &executionErr) {
		return queryExecutionError(serrors.ErrQueryExecutionFailed, "query execution failed")
	}
	switch executionErr.Code {
	case runner.ExecutionConfig:
		return queryExecutionError(serrors.ErrQueryExecutionUnavailable, "query execution is unavailable")
	case runner.ExecutionCapacity, runner.ExecutionClosed:
		return queryExecutionError(serrors.ErrQueryExecutionBusy, "query execution is busy")
	case runner.ExecutionLimit:
		return queryExecutionError(serrors.ErrQueryExecutionLimit, "query execution limit exceeded")
	case runner.ExecutionTimeout:
		return queryExecutionError(serrors.ErrQueryExecutionTimeout, "query execution timed out")
	case runner.ExecutionCancelled:
		return queryExecutionError(serrors.ErrQueryExecutionCancelled, "query execution was cancelled")
	case runner.ExecutionResultContract:
		details := map[string]any{"stage": "result_normalization", "retryable": false}
		if failure := executionErr.ResultContract; failure != nil {
			details["column"] = failure.Column
			details["expected_datatype"] = failure.ExpectedDatatype
			details["observed_family"] = failure.ObservedFamily
		}
		return &serrors.Error{
			Code:    serrors.ErrQueryResultSchemaMismatch,
			Message: "query result does not match the compiled output schema",
			Details: details,
		}
	default:
		return queryExecutionError(serrors.ErrQueryExecutionFailed, "query execution failed")
	}
}
