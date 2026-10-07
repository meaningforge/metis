package validation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/policy"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/doris"
)

type fakeEngine struct {
	scenario                         string
	opens, probes, describes, closes int
}

func (*fakeEngine) DataSourceType() datasource.Type        { return "doris" }
func (*fakeEngine) ValidateConfig(map[string]string) error { return nil }
func (e *fakeEngine) SupportsCompiledValidation(c *artifact.CompiledQuery) bool {
	if e.scenario == "parameters" {
		c.SqlRenderResult.SQL = "malicious factory mutation"
		return true
	}
	return len(c.SqlRenderResult.Parameters) == 0
}
func (e *fakeEngine) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	e.opens++
	return e, nil
}
func (e *fakeEngine) Acquire(context.Context) (driver.Executor, error) { return &fakeLease{e}, nil }
func (*fakeEngine) Close(context.Context) error                        { return nil }

type fakeLease struct{ engine *fakeEngine }

func (*fakeLease) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	panic("validation executed SELECT")
}
func (e *fakeLease) Close() error { e.engine.closes++; return nil }
func (e *fakeLease) DescribeRelation(_ context.Context, ref driver.CatalogReference, _ driver.CatalogLimits) (driver.CatalogRelation, error) {
	e.engine.describes++
	if e.engine.scenario == "permission" {
		return driver.CatalogRelation{}, errors.New("SECRET endpoint private.example access denied")
	}
	p, s := 18, 2
	columns := []driver.CatalogColumn{{Name: "region", NativeType: driver.CatalogNativeType{Name: "STRING"}}, {Name: "amount", NativeType: driver.CatalogNativeType{Name: "DECIMAL", Precision: &p, Scale: &s}}}
	if e.engine.scenario == "policy_column_missing" {
		columns = columns[1:]
	}
	if e.engine.scenario == "missing" {
		columns = columns[:1]
	}
	if e.engine.scenario == "type" {
		columns[1].NativeType = driver.CatalogNativeType{Name: "STRING"}
	}
	return driver.CatalogRelation{Reference: ref, Outcome: "found", ColumnsComplete: true, Columns: columns}, nil
}
func (e *fakeLease) ValidateCompiled(_ context.Context, c *artifact.CompiledQuery, _ driver.CatalogLimits) (driver.ValidationEvidence, error) {
	e.engine.probes++
	if c.SqlRenderResult.Dialect != "DORIS" {
		panic("wrong backend renderer")
	}
	if e.engine.scenario == "parameters" && (c.SqlRenderResult.SQL == "malicious factory mutation" || len(c.SqlRenderResult.Parameters) != 1 || c.SqlRenderResult.Parameters[0].Value != "private-filter-value") {
		panic("factory changed artifact or parameter value")
	}
	if e.engine.scenario == "engine" {
		return driver.ValidationEvidence{}, errors.New("SECRET rejected SQL parameter")
	}
	if e.engine.scenario == "unsupported" {
		return driver.ValidationEvidence{Outcome: "unsupported"}, nil
	}
	return driver.ValidationEvidence{Outcome: "accepted", Method: "explain"}, nil
}

type deniedPolicy struct{}

func (deniedPolicy) Evaluate(context.Context, policy.Request) (policy.Decision, error) {
	return policy.Decision{Effect: policy.Denied}, nil
}

type constrainedPolicy struct{}

func (constrainedPolicy) Evaluate(_ context.Context, req policy.Request) (policy.Decision, error) {
	decision := policy.Decision{Effect: policy.Constrained}
	for _, source := range req.Sources {
		decision.Sources = append(decision.Sources, policy.SourceConstraint{Dataset: source.Dataset, RowPredicates: []policy.Predicate{{Field: policy.FieldRef{Dataset: source.Dataset, Field: "region"}, Operator: query.FilterIsNull}}})
	}
	return decision, nil
}

func TestValidationProductionFlowAndPreflight(t *testing.T) {
	for _, scenario := range []string{"passed", "alias", "parameters", "unsupported_parameters", "policy_column", "policy_column_missing", "compile", "missing", "type", "permission", "engine", "unsupported", "policy", "hidden"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name string, data []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			model, err := os.ReadFile("../../../examples/authoring/live/model-reviewed.ossie.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "alias" {
				model = []byte(strings.ReplaceAll(strings.Replace(string(model), "- name: amount", "- name: net", 1), "SUM(orders.amount)", "SUM(orders.net)"))
			}
			write("model.yaml", model)
			write("project.yaml", []byte("semantic_sources:\n  sales: {path: model.yaml}\n"))
			write("metis.yaml", []byte("version: 1\nprojects:\n  sales:\n    path: project.yaml\n    data_source: warehouse\ndata_sources:\n  path: datasources.yaml\n"))
			write("datasources.yaml", []byte("warehouse:\n  type: doris\n  config: {}\n  policy:\n    max_rows: 100\n    max_bytes: 10000\n    query_timeout: 1s\n"))
			engine := &fakeEngine{scenario: scenario}
			backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: engine})
			if err != nil {
				t.Fatal(err)
			}
			inventory, err := LoadInventory("../../../examples/authoring/live/queries.json", "sales")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "compile" {
				inventory.Queries = append(inventory.Queries, Case{ID: "broken", Query: query.SemanticQuery{Project: "sales", Model: "sales", Metrics: []query.MetricRef{{Name: "absent"}}}})
			}
			if scenario == "parameters" || scenario == "unsupported_parameters" {
				inventory.Queries[0].Query.Filters = []query.Filter{{Field: "region", Operator: query.FilterEQ, Value: "private-filter-value"}}
			}
			if strings.HasPrefix(scenario, "policy_column") {
				inventory.Queries[0].Query.Metrics = []query.MetricRef{{Name: "order_rows"}}
				inventory.Queries[0].Query.Dimensions = nil
			}
			options := []bootstrap.RuntimeOption{bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithBackendRegistry(backends)}
			if scenario == "policy" {
				options = append(options, bootstrap.WithDataAccessPolicy(deniedPolicy{}))
			}
			if strings.HasPrefix(scenario, "policy_column") {
				options = append(options, bootstrap.WithDataAccessPolicy(constrainedPolicy{}))
			}
			if scenario == "hidden" {
				options = append(options, bootstrap.WithAssetVisibilityPolicy(semantic.AssetVisibilityPolicyFunc(func(context.Context, semantic.AssetVisibilityRequest) semantic.AssetVisibilityDecision {
					return semantic.AssetVisibilityDecision{Effect: semantic.AssetVisibilityHidden, Reason: semantic.AssetVisibilityReasonPolicyHidden}
				})))
			}
			ctx := auth.WithPrincipal(context.Background(), &auth.Principal{TenantID: "test", SubjectID: "author"})
			report, err := Run(ctx, filepath.Join(dir, "metis.yaml"), "sales", inventory, options...)
			if err != nil {
				t.Fatal(err)
			}
			if report.Passed != (scenario == "passed" || scenario == "alias" || scenario == "parameters" || scenario == "policy_column") {
				t.Fatalf("report=%+v", report)
			}
			if scenario == "compile" || scenario == "policy" || scenario == "hidden" || scenario == "unsupported_parameters" {
				if engine.opens != 0 {
					t.Fatalf("preflight opened database: %+v", engine)
				}
			}
			if scenario == "missing" || scenario == "type" || scenario == "permission" {
				if engine.probes != 0 {
					t.Fatal("probed after catalog failure")
				}
			}
			if (scenario == "missing" || scenario == "type") && (!report.Complete || !report.Cases[0].CatalogChecked) {
				t.Fatalf("completed catalog mismatch lost coverage: %+v", report)
			}
			if engine.closes != engine.probes+engine.describes {
				t.Fatalf("leases leaked: %+v", engine)
			}
			data, _ := json.Marshal(report)
			for _, secret := range []string{"SECRET", "private.example", "SELECT", "APAC", "private-filter-value"} {
				if strings.Contains(string(data), secret) {
					t.Fatalf("private data escaped: %s", data)
				}
			}
		})
	}
}
