package regression

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/version"
)

type RuntimeOptions struct {
	Project        string
	Config         string
	Backends       *backend.BackendRegistry
	SecretResolver runner.SecretResolver
}

// RunRuntime executes an externally prepared fixture through the production
// query service. It never provisions data or accepts physical query overrides.
func RunRuntime(ctx context.Context, suite Suite, digest string, options RuntimeOptions) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := suite.validate(); err != nil {
		return Report{}, err
	}
	if suite.Fixture == nil || options.Project != suite.Project || options.Config == "" || options.Backends == nil {
		return Report{}, fmt.Errorf("runtime requires fixture, matching project, deployment config, and backends")
	}
	expected := make([]struct {
		ID     string      `json:"id"`
		Expect Expectation `json:"expect"`
	}, len(suite.Cases))
	for i, c := range suite.Cases {
		expected[i].ID, expected[i].Expect = c.ID, c.Expect
	}
	data, err := json.Marshal(expected)
	if err != nil {
		return Report{}, err
	}
	hash := sha256.Sum256(data)
	report := Report{SchemaVersion: 1, Mode: "runtime", Project: suite.Project, Status: "failed", SuiteDigest: digest,
		ExpectationDigest: hex.EncodeToString(hash[:]), MetisVersion: version.Version, Authorization: "local_unrestricted",
		Fixture: suite.Fixture, FixtureVerification: "declared_only", Cases: make([]CaseReport, len(suite.Cases))}
	for i, c := range suite.Cases {
		report.Cases[i] = CaseReport{ID: c.ID, Status: "not_run", ExpectedOutcome: c.Expect.Outcome, ActualOutcome: "not_run"}
	}
	ctx, cancel := context.WithTimeout(ctx, suiteDeadline)
	defer cancel()
	runtime, err := bootstrap.LoadRuntime(options.Config, bootstrap.WithBackendRegistry(options.Backends), bootstrap.WithSecretResolver(options.SecretResolver),
		bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithExecutionCeilings(bootstrap.ExecutionCeilings{MaxRows: maxResultRows, MaxBytes: maxResultBytes, QueryTimeout: caseDeadline}))
	if err != nil {
		markNotRun(&report, "runtime_load_failed")
		var loadErr *source.LoadError
		for i := range report.Cases {
			setRuntimeError(&report.Cases[i], err, nil)
			if errors.As(err, &loadErr) {
				report.Cases[i].Code = string(loadErr.Code)
				report.Cases[i].CallerAction = ""
			}
		}
		return report, nil
	}
	defer runtime.Close(context.Background())
	generation := runtime.Current(options.Project)
	if generation == nil || runtime.Execution == nil {
		markNotRun(&report, "runtime_unavailable")
		return report, nil
	}
	report.CandidateDigest = generation.ContentDigest()
	backendNames := map[string]bool{}
	for _, name := range runtime.Deployment.DataSourcesForProject(options.Project) {
		resolved, err := runtime.Execution.ResolveDataSource(name)
		if err != nil {
			markNotRun(&report, "runtime_unavailable")
			return report, nil
		}
		backendNames[string(resolved.Backend.Type)] = true
	}
	for name := range backendNames {
		report.Backends = append(report.Backends, name)
	}
	sort.Strings(report.Backends)
	ctx = runtime.Generations.Pin(ctx)
	ctx = auth.WithPrincipal(ctx, &auth.Principal{TenantID: "local", SubjectID: "project-regression", Scopes: []string{auth.ScopeSemanticExecute}})
	for i, c := range suite.Cases {
		if ctx.Err() != nil {
			for j := i; j < len(report.Cases); j++ {
				report.Cases[j].Category = "suite_deadline_exceeded"
				if errors.Is(ctx.Err(), context.Canceled) {
					report.Cases[j].Category = "suite_cancelled"
				}
			}
			break
		}
		caseCtx, caseCancel := context.WithTimeout(ctx, caseDeadline)
		request, _ := c.Request.Query.semanticQuery()
		result, queryErr := generation.QueryMetrics.QueryMetrics(caseCtx, semantic.QueryMetricsRequest{Query: request})
		ctxErr := caseCtx.Err()
		caseCancel()
		report.Cases[i] = evaluateRuntime(c, result, queryErr, ctxErr)
	}
	for _, c := range report.Cases {
		switch c.Status {
		case "passed":
			report.Passed++
		case "failed":
			report.Failed++
		default:
			report.NotRun++
		}
	}
	if report.Passed == len(report.Cases) {
		report.Status = "passed"
	}
	return report, nil
}

func evaluateRuntime(c Case, result *semantic.QueryMetricsResult, queryErr, ctxErr error) CaseReport {
	if queryErr != nil || ctxErr != nil || result == nil {
		var semanticErr *serrors.Error
		if ctxErr == nil && errors.As(queryErr, &semanticErr) && !runtimeFailureCode(string(semanticErr.Code)) && serrors.CallerActionOf(semanticErr.Code) != serrors.CallerActionReportDefect {
			return evaluateCompile(c, nil, queryErr, nil)
		}
		report := CaseReport{ID: c.ID, Status: "failed", ExpectedOutcome: c.Expect.Outcome, ActualOutcome: "incomplete", Category: "runtime_unavailable"}
		setRuntimeError(&report, queryErr, ctxErr)
		return report
	}
	// Shared schema and stable-warning assertions; runtime suites forbid SQL snapshots.
	report := evaluateCompile(c, &artifact.CompiledQuery{OutputSchema: result.Schema, Warnings: result.Warnings}, nil, nil)
	if report.Category == "compile_assertion_mismatch" {
		report.Category = "runtime_assertion_mismatch"
	}
	if report.Status != "passed" {
		return report
	}
	if result.Count != int64(len(result.Rows)) || result.Count > maxResultRows || result.Count < 0 {
		report.Status, report.ActualOutcome, report.Category = "failed", "incomplete", "invalid_result_bounds"
		return report
	}
	data, err := json.Marshal(result.Rows)
	if err != nil || len(data) > maxResultBytes {
		report.Status, report.ActualOutcome, report.Category = "failed", "incomplete", "invalid_result_bounds"
		return report
	}
	columns := c.Expect.OutputSchema.Columns
	rows := make([][]Scalar, len(result.Rows))
	for i, row := range result.Rows {
		if len(row) != len(columns) {
			report.Status, report.Category = "failed", "invalid_result_shape"
			return report
		}
		rows[i] = make([]Scalar, len(row))
		for j, value := range row {
			rows[i][j], err = normalizedScalar(value, columns[j].Datatype)
			if err != nil {
				report.Status, report.Category = "failed", "invalid_result_value"
				return report
			}
		}
	}
	if result.Count != *c.Expect.RowCount {
		report.Differences = append(report.Differences, "row_count")
	}
	report.Differences = append(report.Differences, compareRows(*c.Expect.Rows, rows, columns)...)
	if len(report.Differences) > maxReportedDifferences {
		report.Differences = report.Differences[:maxReportedDifferences]
	}
	if len(report.Differences) > 0 {
		report.Status, report.Category = "failed", "runtime_assertion_mismatch"
	}
	return report
}

// Keep only registered stable codes and closed categories, never error text,
// Details, driver strings, or endpoints. These failures cannot satisfy an oracle.
func setRuntimeError(report *CaseReport, err, ctxErr error) {
	var semanticErr *serrors.Error
	if errors.As(err, &semanticErr) {
		for _, code := range serrors.Codes() {
			if semanticErr.Code == code {
				report.Code = string(code)
				report.CallerAction = string(serrors.CallerActionOf(code))
				break
			}
		}
	}
	if report.Category == "runtime_load_failed" {
		return
	}
	switch {
	case errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled):
		report.Code = string(serrors.ErrQueryExecutionCancelled)
	case errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		report.Code = string(serrors.ErrQueryExecutionTimeout)
	}
	if report.Code != "" {
		report.CallerAction = string(serrors.CallerActionOf(serrors.ErrorCode(report.Code)))
	}
	switch serrors.ErrorCode(report.Code) {
	case serrors.ErrQueryExecutionTimeout:
		report.Category = "execution_timeout"
	case serrors.ErrQueryExecutionCancelled:
		report.Category = "execution_cancelled"
	case serrors.ErrQueryExecutionLimit:
		report.Category = "execution_limit_exceeded"
	case serrors.ErrQueryExecutionBusy:
		report.Category = "execution_busy"
	case serrors.ErrQueryExecutionFailed:
		report.Category = "execution_failed"
	case serrors.ErrQueryResultSchemaMismatch:
		report.Category = "result_schema_mismatch"
	case serrors.ErrProjectAccessDenied, serrors.ErrDataAccessDenied:
		report.Category = "access_denied"
	}
}
