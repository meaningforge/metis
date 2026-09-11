package agentquery_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

const model = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.public.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.region}]
            dimension: {}
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`

func TestAgentQueryConformance(t *testing.T) {
	runtime := loadRuntime(t)
	ctx := context.Background()
	zero := 0

	cases := []struct {
		name    string
		request service.CompileRequest
		valid   bool
		code    serrors.ErrorCode
		subject *service.DiagnosticSubject
		repair  string
	}{
		{
			name: "metric dimension query",
			request: service.CompileRequest{Query: query.SemanticQuery{
				Project:    "finance",
				Model:      "sales",
				Metrics:    []query.MetricRef{{Name: "total_revenue"}},
				Dimensions: []query.DimensionRef{{Name: "region"}},
				Filters:    []query.Filter{{Field: "region", Operator: query.FilterEQ, Value: "US"}},
				OrderBy:    []query.OrderBy{{Field: "total_revenue", Direction: query.SortDesc}},
			}, Dialect: "DORIS"},
			valid: true,
		},
		{
			name: "unknown metric with bounded repair evidence",
			request: service.CompileRequest{Query: query.SemanticQuery{
				Project: "finance", Model: "sales",
				Metrics: []query.MetricRef{{Name: "revenue"}},
			}, Dialect: "DORIS"},
			code:    serrors.ErrMetricNotFound,
			subject: &service.DiagnosticSubject{Kind: "metric", Name: "revenue"},
			repair:  "sales.total_revenue",
		},
		{
			name: "unknown dimension",
			request: service.CompileRequest{Query: query.SemanticQuery{
				Project:    "finance",
				Model:      "sales",
				Metrics:    []query.MetricRef{{Name: "total_revenue"}},
				Dimensions: []query.DimensionRef{{Name: "country"}},
			}, Dialect: "DORIS"},
			code: serrors.ErrDimensionNotFound,
		},
		{
			name: "invalid filter operator",
			request: service.CompileRequest{Query: query.SemanticQuery{
				Project: "finance", Model: "sales",
				Metrics: []query.MetricRef{{Name: "total_revenue"}},
				Filters: []query.Filter{{Field: "region", Operator: query.FilterOperator("contains"), Value: "US"}},
			}, Dialect: "DORIS"},
			code: serrors.ErrInvalidQuery,
		},
		{
			name: "non-positive limit",
			request: service.CompileRequest{Query: query.SemanticQuery{
				Project: "finance", Model: "sales",
				Metrics: []query.MetricRef{{Name: "total_revenue"}}, Limit: &zero,
			}, Dialect: "DORIS"},
			code: serrors.ErrInvalidQuery,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validation, err := runtime.Compile.Validate(ctx, tc.request)
			if err != nil {
				t.Fatal(err)
			}
			if validation.Valid != tc.valid {
				t.Fatalf("valid = %v, want %v; diagnostics=%#v", validation.Valid, tc.valid, validation.Diagnostics)
			}
			if tc.valid {
				if len(validation.Diagnostics) != 0 {
					t.Fatalf("unexpected diagnostics: %#v", validation.Diagnostics)
				}
				if _, err := runtime.Compile.Explain(ctx, tc.request); err != nil {
					t.Fatalf("explain: %v", err)
				}
				if _, err := runtime.Compile.Compile(ctx, tc.request); err != nil {
					t.Fatalf("compile: %v", err)
				}
				return
			}

			if len(validation.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", validation.Diagnostics)
			}
			diagnostic := validation.Diagnostics[0]
			if diagnostic.Code != tc.code {
				t.Fatalf("code = %s, want %s", diagnostic.Code, tc.code)
			}
			if tc.subject != nil {
				if diagnostic.Subject == nil || *diagnostic.Subject != *tc.subject {
					t.Fatalf("subject = %#v, want %#v", diagnostic.Subject, tc.subject)
				}
			}
			if tc.repair != "" && !hasSuggestion(diagnostic.Suggestions, tc.repair) {
				t.Fatalf("suggestions = %#v, want %q", diagnostic.Suggestions, tc.repair)
			}

			_, explainErr := runtime.Compile.Explain(ctx, tc.request)
			_, compileErr := runtime.Compile.Compile(ctx, tc.request)
			if explainErr == nil || compileErr == nil {
				t.Fatalf("expected explain/compile errors: explain=%v compile=%v", explainErr, compileErr)
			}
			if got := serrors.PayloadFrom(explainErr).Code; got != string(tc.code) {
				t.Fatalf("explain code = %s, want %s", got, tc.code)
			}
			if got := serrors.PayloadFrom(compileErr).Code; got != string(tc.code) {
				t.Fatalf("compile code = %s, want %s", got, tc.code)
			}
		})
	}
}

func hasSuggestion(suggestions []service.SemanticSuggestion, value string) bool {
	for _, suggestion := range suggestions {
		if suggestion.Value == value {
			return true
		}
	}
	return false
}

func loadRuntime(t *testing.T) *bootstrap.ProjectRuntime {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sales.ossie.yaml"), model)
	writeFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  sales:\n    path: ./sales.ossie.yaml\n")
	writeFile(t, filepath.Join(dir, "metis.yaml"), "version: 1\nprojects:\n  finance:\n    path: ./project.yaml\n")
	runtime, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"), bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
