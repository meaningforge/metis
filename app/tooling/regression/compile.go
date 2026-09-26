package regression

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/version"
)

const (
	caseDeadline           = 30 * time.Second
	suiteDeadline          = 10 * time.Minute
	maxReportedColumns     = 100
	maxReportedDifferences = 10
)

type CompileOptions struct {
	Project string
	Config  string
	Dialect sql.SQLDialect
}

type Report struct {
	SchemaVersion       int            `json:"schema_version"`
	Mode                string         `json:"mode"`
	Project             string         `json:"project"`
	Dialect             sql.SQLDialect `json:"dialect,omitempty"`
	Status              string         `json:"status"`
	SuiteDigest         string         `json:"suite_digest"`
	ExpectationDigest   string         `json:"expectation_digest"`
	CandidateDigest     string         `json:"candidate_digest,omitempty"`
	MetisVersion        string         `json:"metis_version"`
	Authorization       string         `json:"authorization"`
	Fixture             *Fixture       `json:"fixture,omitempty"`
	FixtureVerification string         `json:"fixture_verification,omitempty"`
	Backends            []string       `json:"backends,omitempty"`
	Passed              int            `json:"passed"`
	Failed              int            `json:"failed"`
	NotRun              int            `json:"not_run"`
	Cases               []CaseReport   `json:"cases"`
}

type CaseReport struct {
	ID              string          `json:"id"`
	Status          string          `json:"status"`
	ExpectedOutcome string          `json:"expected_outcome"`
	ActualOutcome   string          `json:"actual_outcome"`
	Category        string          `json:"category,omitempty"`
	Code            string          `json:"code,omitempty"`
	CallerAction    string          `json:"caller_action,omitempty"`
	ExpectedSchema  *ExpectedSchema `json:"expected_schema,omitempty"`
	ActualSchema    *ExpectedSchema `json:"actual_schema,omitempty"`
	SchemaTruncated bool            `json:"schema_truncated,omitempty"`
	Differences     []string        `json:"differences,omitempty"`
}

// RunCompile compiles against one immutable offline candidate. It never
// constructs a DataSource registry, resolves secrets, or opens a connection.
func RunCompile(ctx context.Context, suite Suite, suiteDigest string, options CompileOptions) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := suite.validate(); err != nil {
		return Report{}, err
	}
	if suite.Fixture != nil {
		return Report{}, fmt.Errorf("compile mode does not accept a runtime fixture")
	}
	if options.Project != suite.Project || options.Config == "" || options.Dialect == "" {
		return Report{}, fmt.Errorf("project, config, and dialect are required; project must match the suite")
	}
	options.Dialect = renderer.NormalizeSQLDialect(string(options.Dialect))
	renderers, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		return Report{}, err
	}
	if _, err := renderers.Resolve(options.Dialect); err != nil {
		return Report{}, err
	}
	for _, c := range suite.Cases {
		if c.Expect.SQLRenderResult != nil && c.Expect.SQLRenderResult.Dialect != string(options.Dialect) {
			return Report{}, fmt.Errorf("case %q: SQL snapshot dialect must match --dialect", c.ID)
		}
		if c.Expect.SQLRenderResult != nil && c.Expect.SQLRenderResult.CompilerVersion != version.Version {
			return Report{}, fmt.Errorf("case %q: SQL snapshot compiler_version must match this Metis build", c.ID)
		}
	}
	expectedCases := make([]struct {
		ID     string      `json:"id"`
		Expect Expectation `json:"expect"`
	}, len(suite.Cases))
	for i, c := range suite.Cases {
		expectedCases[i].ID = c.ID
		expectedCases[i].Expect = c.Expect
	}
	expectations, err := json.Marshal(expectedCases)
	if err != nil {
		return Report{}, err
	}
	expectationHash := sha256.Sum256(expectations)
	report := Report{
		SchemaVersion: 1, Mode: "compile", Project: suite.Project, Dialect: options.Dialect,
		Status: "failed", SuiteDigest: suiteDigest, ExpectationDigest: hex.EncodeToString(expectationHash[:]),
		MetisVersion: version.Version, Authorization: "local_unrestricted",
		Cases: make([]CaseReport, len(suite.Cases)),
	}
	for i, c := range suite.Cases {
		report.Cases[i] = CaseReport{ID: c.ID, Status: "not_run", ExpectedOutcome: c.Expect.Outcome, ActualOutcome: "not_run"}
	}
	ctx, cancel := context.WithTimeout(ctx, suiteDeadline)
	defer cancel()
	candidate, err := source.LoadProject(options.Project, options.Config)
	if err != nil {
		markNotRun(&report, "candidate_load_failed")
		var loadErr *source.LoadError
		if errors.As(err, &loadErr) {
			for i := range report.Cases {
				report.Cases[i].Code = string(loadErr.Code)
			}
		}
		return report, nil
	}
	report.CandidateDigest = candidate.Bundle.ContentDigest
	runtime, err := bootstrap.NewRuntime(ctx, bootstrap.RuntimeInput{
		Projects: map[string]bootstrap.ProjectInput{
			options.Project: {Config: candidate.Config, Documents: candidate.Bundle.Documents},
		},
	}, bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		markNotRun(&report, "candidate_assembly_failed")
		return report, nil
	}
	defer runtime.Close(context.Background())
	for i, c := range suite.Cases {
		if err := ctx.Err(); err != nil {
			for j := i; j < len(report.Cases); j++ {
				report.Cases[j].Category = "suite_deadline_exceeded"
				if errors.Is(err, context.Canceled) {
					report.Cases[j].Category = "suite_cancelled"
				}
			}
			break
		}
		caseCtx, caseCancel := context.WithTimeout(ctx, caseDeadline)
		request, _ := c.Request.Query.semanticQuery() // validated by suite.validate
		compiled, compileErr := runtime.Compile.Compile(caseCtx, semantic.CompileRequest{Query: request, Dialect: options.Dialect})
		caseCtxErr := caseCtx.Err()
		caseCancel()
		report.Cases[i] = evaluateCompile(c, compiled, compileErr, caseCtxErr)
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

func markNotRun(report *Report, category string) {
	for i := range report.Cases {
		report.Cases[i].Category = category
	}
	report.NotRun = len(report.Cases)
}

func evaluateCompile(c Case, compiled *artifact.CompiledQuery, compileErr, caseCtxErr error) CaseReport {
	result := CaseReport{ID: c.ID, Status: "failed", ExpectedOutcome: c.Expect.Outcome}
	if caseCtxErr != nil {
		result.ActualOutcome = "incomplete"
		result.Category = "case_deadline_exceeded"
		if errors.Is(caseCtxErr, context.Canceled) {
			result.Category = "case_cancelled"
		}
		return result
	}
	if compileErr != nil {
		var semanticErr *serrors.Error
		if !errors.As(compileErr, &semanticErr) || serrors.CallerActionOf(semanticErr.Code) == serrors.CallerActionReportDefect {
			result.ActualOutcome = "incomplete"
			result.Category = "compilation_unavailable"
			return result
		}
		result.ActualOutcome = "semantic_error"
		result.Code = string(semanticErr.Code)
		result.CallerAction = string(serrors.CallerActionOf(semanticErr.Code))
		if c.Expect.Outcome == "semantic_error" && result.Code == c.Expect.Code && result.CallerAction == c.Expect.CallerAction {
			result.Status = "passed"
			return result
		}
		result.Category = "unexpected_semantic_error"
		return result
	}
	if compiled == nil {
		result.ActualOutcome = "incomplete"
		result.Category = "compilation_unavailable"
		return result
	}
	result.ActualOutcome = "success"
	if c.Expect.Outcome != "success" {
		result.Category = "unexpected_success"
		return result
	}
	actualSchema := ExpectedSchema{Columns: make([]ExpectedColumn, len(compiled.OutputSchema.Columns))}
	for i, col := range compiled.OutputSchema.Columns {
		actualSchema.Columns[i] = ExpectedColumn{Name: col.Name, Kind: string(col.Kind), Datatype: string(col.Datatype), Grain: col.Grain}
	}
	if !reflect.DeepEqual(*c.Expect.OutputSchema, actualSchema) {
		result.Differences = append(result.Differences, schemaDifferences(*c.Expect.OutputSchema, actualSchema)...)
		expectedColumns := c.Expect.OutputSchema.Columns
		actualColumns := actualSchema.Columns
		if len(expectedColumns) > maxReportedColumns {
			expectedColumns = expectedColumns[:maxReportedColumns]
			result.SchemaTruncated = true
		}
		if len(actualColumns) > maxReportedColumns {
			actualColumns = actualColumns[:maxReportedColumns]
			result.SchemaTruncated = true
		}
		result.ExpectedSchema = &ExpectedSchema{Columns: expectedColumns}
		result.ActualSchema = &ExpectedSchema{Columns: actualColumns}
	}
	if c.Expect.Warnings != nil {
		actual := make([]ExpectedWarn, len(compiled.Warnings))
		for i, warning := range compiled.Warnings {
			actual[i] = ExpectedWarn{Code: warning.Code, AssetRef: warning.AssetRef, DeprecationDate: warning.DeprecationDate, Replacement: warning.Replacement}
		}
		if !reflect.DeepEqual(*c.Expect.Warnings, actual) {
			result.Differences = append(result.Differences, "warnings")
		}
	}
	if snapshot := c.Expect.SQLRenderResult; snapshot != nil {
		if snapshot.Dialect != string(compiled.SqlRenderResult.Dialect) || snapshot.SQL != compiled.SqlRenderResult.SQL {
			result.Differences = append(result.Differences, "sql_text")
		}
		if !parametersMatch(snapshot.Parameters, compiled.SqlRenderResult.Parameters) {
			result.Differences = append(result.Differences, "sql_parameters")
		}
	}
	if len(result.Differences) != 0 {
		result.Category = "compile_assertion_mismatch"
		return result
	}
	result.Status = "passed"
	return result
}

func schemaDifferences(expected, actual ExpectedSchema) []string {
	differences := make([]string, 0, maxReportedDifferences)
	if len(expected.Columns) != len(actual.Columns) {
		differences = append(differences, "output_schema.columns.length")
	}
	limit := min(len(expected.Columns), len(actual.Columns))
	for i := 0; i < limit && len(differences) < maxReportedDifferences; i++ {
		if !reflect.DeepEqual(expected.Columns[i], actual.Columns[i]) {
			differences = append(differences, fmt.Sprintf("output_schema.columns[%d]", i))
		}
	}
	return differences
}

func parametersMatch(expected []ExpectedParameter, actual []sql.QueryParameter) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, p := range actual {
		if expected[i].Name != p.Name || expected[i].Type != parameterType(p.Value) {
			return false
		}
		if number, ok := p.Value.(json.Number); ok {
			if expected[i].Value != string(number) {
				return false
			}
			continue
		}
		want, wantErr := json.Marshal(expected[i].Value)
		got, gotErr := json.Marshal(p.Value)
		if wantErr != nil || gotErr != nil || !bytes.Equal(want, got) {
			return false
		}
	}
	return true
}

func parameterType(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32:
		if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
			return "unsupported"
		}
		return "float"
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return "unsupported"
		}
		return "float"
	case json.Number:
		return "decimal"
	case []byte:
		return "bytes"
	default:
		return "unsupported"
	}
}
