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

func TestValidationProductionFlowAndPreflight(t *testing.T) {
	for _, scenario := range []string{"passed", "compile", "missing", "type", "permission", "engine", "unsupported", "policy", "hidden"} {
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
			options := []bootstrap.RuntimeOption{bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithBackendRegistry(backends)}
			if scenario == "policy" {
				options = append(options, bootstrap.WithDataAccessPolicy(deniedPolicy{}))
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
			if report.Passed != (scenario == "passed") {
				t.Fatalf("report=%+v", report)
			}
			if scenario == "compile" || scenario == "policy" || scenario == "hidden" {
				if engine.opens != 0 {
					t.Fatalf("preflight opened database: %+v", engine)
				}
			}
			if scenario == "missing" || scenario == "type" || scenario == "permission" {
				if engine.probes != 0 {
					t.Fatal("probed after catalog failure")
				}
			}
			if engine.closes != engine.probes+engine.describes {
				t.Fatalf("leases leaked: %+v", engine)
			}
			data, _ := json.Marshal(report)
			for _, secret := range []string{"SECRET", "private.example", "SELECT", "APAC"} {
				if strings.Contains(string(data), secret) {
					t.Fatalf("private data escaped: %s", data)
				}
			}
		})
	}
}
