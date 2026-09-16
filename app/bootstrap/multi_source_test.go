package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/doris"
)

type placementDriverFactory struct{}

func (placementDriverFactory) DataSourceType() datasource.Type        { return "doris" }
func (placementDriverFactory) ValidateConfig(map[string]string) error { return nil }
func (placementDriverFactory) OpenDataSource(_ context.Context, request driver.OpenRequest) (driver.Runtime, error) {
	return placementRuntime{value: request.Config["value"]}, nil
}

type placementRuntime struct{ value string }

func (r placementRuntime) Acquire(context.Context) (driver.Executor, error) {
	return placementExecutor{value: r.value}, nil
}
func (placementRuntime) Close(context.Context) error { return nil }

type placementExecutor struct{ value string }

func (e placementExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	return &placementStream{value: e.value}, nil
}
func (placementExecutor) Close() error { return nil }

type placementStream struct {
	value string
	sent  bool
}

func (s *placementStream) Next(context.Context) ([]any, error) {
	if s.sent {
		return nil, io.EOF
	}
	s.sent = true
	return []any{json.Number(s.value)}, nil
}
func (*placementStream) Close() error { return nil }

func TestMemoryRuntimeRoutesSemanticModelsToAppliedDataSources(t *testing.T) {
	input := multiSourceInput(t, true)
	runtime, err := bootstrap.NewRuntime(context.Background(), input, bootstrap.WithBackendRegistry(placementBackends(t)), bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	for _, test := range []struct {
		model, metric, want string
	}{{"sales", "revenue", "101"}, {"customers", "customer_count", "202"}} {
		result, err := runtime.QueryMetrics.QueryMetrics(ctx, semantic.QueryMetricsRequest{Query: query.SemanticQuery{Project: "analytics", Model: test.model, Metrics: []query.MetricRef{{Name: test.metric}}}})
		if err != nil {
			t.Fatalf("query %s: %v", test.model, err)
		}
		if result.Count != 1 || len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != test.want {
			t.Fatalf("query %s result = %#v", test.model, result)
		}
	}
}

func TestMemoryRuntimeRequiresCompleteMultiSourcePlacement(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*bootstrap.RuntimeInput)
	}{
		{name: "missing model placement", mutate: func(input *bootstrap.RuntimeInput) {
			p := input.Projects["analytics"]
			p.Documents[1].Content = modelDocument("customers", "customer_count", "", "customers")
			input.Projects["analytics"] = p
		}},
		{name: "unapplied source", mutate: func(input *bootstrap.RuntimeInput) {
			p := input.Projects["analytics"]
			p.Documents[1].Content = modelDocument("customers", "customer_count", "missing", "customers")
			input.Projects["analytics"] = p
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := multiSourceInput(t, true)
			test.mutate(&input)
			runtime, err := bootstrap.NewRuntime(context.Background(), input, bootstrap.WithBackendRegistry(placementBackends(t)), bootstrap.WithLocalAllAccessProjectAuthorization())
			if err == nil {
				runtime.Close(context.Background())
				t.Fatal("invalid multi-source placement accepted")
			}
		})
	}
}

func TestMemoryRuntimeInfersSoleAppliedDataSource(t *testing.T) {
	input := multiSourceInput(t, false)
	p := input.Projects["analytics"]
	p.DataSources = []string{"sales-db"}
	p.Documents = p.Documents[:1]
	p.Documents[0].Content = modelDocument("sales", "revenue", "", "orders")
	p.Config.SemanticSources = map[string]execution.SemanticSourceConfig{"sales": {Path: "sales.yaml"}}
	input.Projects["analytics"] = p
	delete(input.DataSources, "customer-db")
	runtime, err := bootstrap.NewRuntime(context.Background(), input, bootstrap.WithBackendRegistry(placementBackends(t)), bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		t.Fatal(err)
	}
	runtime.Close(context.Background())
}

func multiSourceInput(t *testing.T, explicit bool) bootstrap.RuntimeInput {
	t.Helper()
	config, err := execution.LoadProjectConfig([]byte("semantic_sources:\n  sales: {path: sales.yaml}\n  customers: {path: customers.yaml}\n"))
	if err != nil {
		t.Fatal(err)
	}
	salesSource, customerSource := "", ""
	if explicit {
		salesSource, customerSource = "sales-db", "customer-db"
	}
	maxRows, maxBytes, maxConcurrency := int64(10), int64(1024), 1
	policy := datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: &maxConcurrency}
	return bootstrap.RuntimeInput{
		Projects: map[string]bootstrap.ProjectInput{"analytics": {
			Config: config, DataSources: []string{"sales-db", "customer-db"},
			Documents: []source.SourceDocument{
				{Source: "sales", Path: "sales.yaml", Content: modelDocument("sales", "revenue", salesSource, "orders")},
				{Source: "customers", Path: "customers.yaml", Content: modelDocument("customers", "customer_count", customerSource, "customers")},
			},
		}},
		DataSources: map[string]datasource.DataSource{
			"sales-db":    {Type: "doris", Config: map[string]string{"value": "101"}, Policy: policy},
			"customer-db": {Type: "doris", Config: map[string]string{"value": "202"}, Policy: policy},
		},
	}
}

func modelDocument(model, metric, dataSource, table string) []byte {
	extension := ""
	if dataSource != "" {
		extension = fmt.Sprintf("    custom_extensions:\n      - vendor_name: METIS\n        data: '{\"kind\":\"data_source\",\"name\":\"%s\"}'\n", dataSource)
	}
	return []byte(fmt.Sprintf(`version: "0.2.0.dev0"
semantic_model:
  - name: %s
%s    datasets:
      - name: %s
        source: analytics.%s
        fields:
          - name: value
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: value}]}
    metrics:
      - name: %s
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(%s.value)}]}
`, model, extension, table, table, metric, table))
}

func placementBackends(t *testing.T) *backend.BackendRegistry {
	t.Helper()
	registry, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: placementDriverFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
