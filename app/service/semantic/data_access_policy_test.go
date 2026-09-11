package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/service/policy"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

type dataPolicyFunc func(context.Context, policy.Request) (policy.Decision, error)

func (f dataPolicyFunc) Evaluate(ctx context.Context, req policy.Request) (policy.Decision, error) {
	return f(ctx, req)
}
func dataPolicyContext() context.Context {
	return auth.WithPrincipal(context.Background(), &auth.Principal{TenantID: "tenant", SubjectID: "subject", APIKeyID: "key", Scopes: []string{auth.ScopeAll}})
}
func channelDataPolicy(req policy.Request) policy.Decision {
	out := policy.Decision{Effect: policy.Constrained}
	for _, source := range req.Sources {
		out.Sources = append(out.Sources, policy.SourceConstraint{Dataset: source.Dataset, RowPredicates: []policy.Predicate{{Field: policy.FieldRef{Dataset: source.Dataset, Field: "channel"}, Operator: query.FilterEQ, Values: []any{"private-policy-value"}}}})
	}
	return out
}
func assertPolicyQuery(t *testing.T, q sql.SqlRenderResult) {
	t.Helper()
	if !strings.Contains(q.SQL, "(SELECT * FROM") || strings.Contains(q.SQL, "private-policy-value") {
		t.Fatalf("policy not safely parameterized: %s", q.SQL)
	}
	found := false
	for _, parameter := range q.Parameters {
		if parameter.Value == "private-policy-value" {
			found = true
		}
	}
	if !found {
		t.Fatal("policy parameter missing")
	}
}

func TestCompileValidateExplainShareDataPolicy(t *testing.T) {
	s := attributionServiceForTest(t).compile
	calls := 0
	s.WithDataAccessPolicy(dataPolicyFunc(func(_ context.Context, req policy.Request) (policy.Decision, error) {
		calls++
		return channelDataPolicy(req), nil
	}))
	req := CompileRequest{Dialect: "DORIS", Query: query.SemanticQuery{Project: "analytics", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "orders.region"}}}}
	compiled, err := s.Compile(dataPolicyContext(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicyQuery(t, compiled.SqlRenderResult)
	for _, column := range compiled.OutputSchema.Columns {
		if strings.Contains(column.Name, "channel") {
			t.Fatal("policy-only field leaked into output")
		}
	}
	if _, err := s.Validate(dataPolicyContext(), req); err != nil {
		t.Fatal(err)
	}
	explanation, err := s.Explain(dataPolicyContext(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicyQuery(t, explanation.SqlRenderResult)
	if !reflect.DeepEqual(explanation.SqlRenderResult, compiled.SqlRenderResult) {
		t.Fatal("Explain and Compile policy SQL differ")
	}
	data, err := json.Marshal(explanation.QueryExplanation)
	if err != nil {
		t.Fatal(err)
	}
	if !explanation.DataConstraintsApplied || strings.Contains(string(data), "private-policy-value") || strings.Contains(string(data), "channel") {
		t.Fatalf("explain leaked policy: %s", data)
	}
	if calls != 3 {
		t.Fatalf("evaluations=%d want one per operation", calls)
	}
}

func TestComparisonAndAttributionEvaluateOneSnapshot(t *testing.T) {
	t.Run("comparison", func(t *testing.T) {
		s := comparisonServiceForTest(t)
		calls := 0
		s.compile.WithDataAccessPolicy(dataPolicyFunc(func(_ context.Context, req policy.Request) (policy.Decision, error) {
			calls++
			return channelDataPolicy(req), nil
		}))
		normalized, err := s.normalizeComparison(comparisonQueryForTest())
		if err != nil {
			t.Fatal(err)
		}
		baseline, current, _, err := s.compileComparison(dataPolicyContext(), normalized, comparisonRendererForTest(t))
		if err != nil {
			t.Fatal(err)
		}
		assertPolicyQuery(t, baseline.SqlRenderResult)
		assertPolicyQuery(t, current.SqlRenderResult)
		if calls != 1 {
			t.Fatalf("evaluations=%d", calls)
		}
	})
	t.Run("attribution", func(t *testing.T) {
		s := attributionServiceForTest(t)
		calls := 0
		s.compile.WithDataAccessPolicy(dataPolicyFunc(func(_ context.Context, req policy.Request) (policy.Decision, error) {
			calls++
			return channelDataPolicy(req), nil
		}))
		normalized, err := s.normalize(additiveAttributionQueryForTest())
		if err != nil {
			t.Fatal(err)
		}
		bundle, _, _, err := s.compileBundle(dataPolicyContext(), normalized, attributionRendererForTest(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(bundle.Queries) != 2 {
			t.Fatal("missing attribution dimensions")
		}
		for _, compiled := range bundle.Queries {
			assertPolicyQuery(t, compiled.SqlRenderResult)
		}
		if calls != 1 {
			t.Fatalf("evaluations=%d", calls)
		}
	})
}

func TestDataPolicyPreflightStopsAllExecution(t *testing.T) {
	for _, failure := range []string{"denied", "unavailable", "invalid", "column"} {
		t.Run(failure, func(t *testing.T) {
			state := &attributionDriverState{}
			s := executableAttributionServiceForTest(t, state, 100)
			s.compile.WithDataAccessPolicy(dataPolicyFunc(func(_ context.Context, req policy.Request) (policy.Decision, error) {
				switch failure {
				case "denied":
					return policy.Decision{Effect: policy.Denied}, nil
				case "unavailable":
					return policy.Decision{}, errors.New("private-policy-value")
				case "invalid":
					return policy.Decision{Effect: policy.Constrained}, nil
				default:
					decision := channelDataPolicy(req)
					decision.Sources[0].DeniedFields = []policy.FieldRef{{Dataset: decision.Sources[0].Dataset, Field: "amount"}}
					return decision, nil
				}
			}))
			result, err := s.AttributeMetric(dataPolicyContext(), additiveAttributionQueryForTest())
			if err == nil || result != nil || state.calls != 0 {
				t.Fatalf("preflight escaped: result=%v err=%v calls=%d", result, err, state.calls)
			}
			payload, marshalErr := json.Marshal(serrors.PayloadFrom(err))
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if strings.Contains(string(payload), "private-policy-value") || strings.Contains(string(payload), "amount") {
				t.Fatalf("private error evidence escaped: %s", payload)
			}
		})
	}
}

func TestNilDataPolicyNeverBecomesUnrestricted(t *testing.T) {
	s := attributionServiceForTest(t).compile.WithDataAccessPolicy(nil)
	_, err := s.Compile(dataPolicyContext(), CompileRequest{Dialect: "DORIS", Query: query.SemanticQuery{Project: "analytics", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}}}})
	if errorCode(err) != serrors.ErrDataAccessPolicyUnavailable {
		t.Fatalf("nil adapter: %v", err)
	}
}

func TestEarlierAuthorizationNeverInvokesDataPolicy(t *testing.T) {
	for _, boundary := range []string{"project", "asset"} {
		t.Run(boundary, func(t *testing.T) {
			s := attributionServiceForTest(t).compile
			calls := 0
			s.WithDataAccessPolicy(dataPolicyFunc(func(context.Context, policy.Request) (policy.Decision, error) {
				calls++
				return policy.Decision{Effect: policy.Unrestricted}, nil
			}))
			if boundary == "project" {
				s.WithProjectAuthorizer(ProjectAuthorizerFunc(func(context.Context, ProjectAuthorizationRequest) ProjectAuthorizationDecision {
					return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonPolicyDenied}
				}))
			} else {
				s.discovery.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
					return AssetVisibilityDecision{Effect: AssetVisibilityHidden, Reason: AssetVisibilityReasonPolicyHidden}
				}))
			}
			_, err := s.Compile(dataPolicyContext(), CompileRequest{Dialect: "DORIS", Query: query.SemanticQuery{Project: "analytics", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}}}})
			if err == nil || calls != 0 {
				t.Fatalf("err=%v policy calls=%d", err, calls)
			}
		})
	}
}

func TestAllExecutionEntrypointsRejectBeforeBackend(t *testing.T) {
	for _, operation := range []string{"query_metrics", "get_dimension_values", "compare_metrics"} {
		t.Run(operation, func(t *testing.T) {
			state := &attributionDriverState{}
			base := executableAttributionServiceForTest(t, state, 100)
			calls := 0
			base.compile.WithDataAccessPolicy(dataPolicyFunc(func(_ context.Context, _ policy.Request) (policy.Decision, error) {
				calls++
				return policy.Decision{Effect: policy.Denied}, nil
			}))
			var err error
			switch operation {
			case "query_metrics":
				s := NewQueryMetricsService(base.compile, base.projects, base.runtime)
				_, err = s.QueryMetrics(dataPolicyContext(), QueryMetricsRequest{Query: query.SemanticQuery{Project: "analytics", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}}}})
			case "get_dimension_values":
				s := NewDimensionValuesService(base.compile, base.discovery, base.projects, base.runtime)
				_, err = s.GetDimensionValues(dataPolicyContext(), DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region"})
			case "compare_metrics":
				s := NewCompareMetricsService(base.compile, base.discovery, base.projects, base.runtime)
				_, err = s.CompareMetrics(dataPolicyContext(), comparisonQueryForTest())
			}
			if errorCode(err) != serrors.ErrDataAccessDenied || calls != 1 || state.calls != 0 {
				t.Fatalf("err=%v evaluations=%d executions=%d", err, calls, state.calls)
			}
		})
	}
}
