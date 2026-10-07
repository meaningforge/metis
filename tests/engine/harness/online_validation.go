package harness

import (
	"context"
	"encoding/json"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/policy"
	executionbackend "github.com/meaningforge/metis/execution/backend"
	clickhousebackend "github.com/meaningforge/metis/execution/backend/clickhouse"
	dorisbackend "github.com/meaningforge/metis/execution/backend/doris"
	"github.com/meaningforge/metis/execution/runner"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/tooling/validation"
	"github.com/meaningforge/metis/query"
)

func checkOnlineValidationFailures(t *testing.T, ctx context.Context, binary, work, model string, reviewed []byte, backend string) {
	t.Helper()
	base, err := validation.LoadInventory(filepath.Join(work, "queries.json"), "sales")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing_column", "type_mismatch", "parameter", "parameter_in", "parameter_decimal", "parameter_datetime", "parameter_example", "compile_failure"} {
		t.Run("online_"+scenario, func(t *testing.T) {
			inventory := base
			inventory.Queries = append([]validation.Case(nil), base.Queries...)
			body := reviewed
			switch scenario {
			case "missing_column":
				body = []byte(strings.Replace(string(reviewed), "expression: orders.amount", "expression: orders.metis_missing_amount", 1))
			case "type_mismatch":
				body = []byte(strings.Replace(string(reviewed), "datatype: String", "datatype: Integer", 1))
			case "parameter":
				inventory.Queries[0].Query.Filters = []query.Filter{{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
			case "parameter_in":
				inventory.Queries[0].Query.Filters = []query.Filter{{Field: "region", Operator: query.FilterIN, Value: []any{"APAC", "EMEA' -- ? {foreign:String}"}}}
			case "parameter_decimal":
				inventory.Queries[0].Query.Filters = []query.Filter{{Field: "total_revenue", Operator: query.FilterBetween, Value: []any{0.25, 20.75}}}
			case "parameter_datetime":
				body = []byte(strings.Replace(string(reviewed), "expression: orders.order_time}]", "expression: orders.order_time}]\n            dimension: {}", 1))
				inventory.Queries[0].Query.Filters = []query.Filter{{Field: "orders.order_time", Operator: query.FilterBetween, Value: []any{"2026-01-01 00:00:00", "2026-01-03 00:00:00"}}}
			case "parameter_example":
				inventory, err = validation.LoadInventory(filepath.Join(work, "queries-filtered.json"), "sales")
				if err != nil {
					t.Fatal(err)
				}
			case "compile_failure":
				inventory.Queries = append(inventory.Queries, validation.Case{ID: "broken", Query: query.SemanticQuery{Project: "sales", Model: "sales", Metrics: []query.MetricRef{{Name: "absent_metric"}}}})
			}
			if err := os.WriteFile(model, body, 0600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.WriteFile(model, reviewed, 0600); err != nil {
					t.Error(err)
				}
			}()
			input := filepath.Join(work, scenario+"-queries.json")
			data, _ := json.Marshal(inventory)
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(work, scenario+"-report.json")
			cmd := exec.CommandContext(ctx, binary, "project", "validate", "--online", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--queries", input, "--output", output)
			_, commandErr := cmd.CombinedOutput()
			data, err = os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var report validation.Report
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(scenario, "parameter") && backend == "clickhouse" {
				if commandErr != nil || !report.Passed || !report.Cases[0].EnginePrepared {
					t.Fatalf("server-bound validation failed: %s", data)
				}
				return
			}
			if commandErr == nil || report.Passed {
				t.Fatalf("accepted broken validation: %s", data)
			}
			if strings.HasPrefix(scenario, "parameter") && (report.Cases[0].Outcome != "unsupported" || report.Cases[0].CatalogChecked) {
				t.Fatalf("unsafe parameter probe: %s", data)
			}
			if scenario == "compile_failure" {
				for _, c := range report.Cases {
					if c.CatalogChecked || c.EnginePrepared {
						t.Fatalf("partial workload probed: %s", data)
					}
				}
			}
		})
	}
	t.Run("online_policy_parameters", func(t *testing.T) {
		registry, err := executionbackend.NewBackendRegistry(dorisbackend.New(), clickhousebackend.New())
		if err != nil {
			t.Fatal(err)
		}
		principalCtx := auth.WithPrincipal(ctx, &auth.Principal{TenantID: "test", SubjectID: "policy-author", Scopes: []string{auth.ScopeSemanticAuthor, auth.ScopeSemanticCompile}})
		report, err := validation.Run(principalCtx, filepath.Join(work, "metis.yaml"), "sales", base, bootstrap.WithBackendRegistry(registry), bootstrap.WithSecretResolver(runner.NewEnvSecretResolver()), bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithDataAccessPolicy(validationRegionPolicy{}))
		if err != nil {
			t.Fatal(err)
		}
		if backend == "clickhouse" {
			if !report.Passed || !report.Complete {
				data, _ := json.Marshal(report)
				t.Fatalf("server-bound policy validation: %s", data)
			}
		} else if report.Passed || report.Cases[0].Outcome != "unsupported" {
			t.Fatal("Doris policy parameters falsely accepted")
		}
		data, _ := json.Marshal(report)
		if strings.Contains(string(data), "private-policy-tenant") {
			t.Fatal("policy value escaped report")
		}
	})
	t.Run("online_bad_credentials", func(t *testing.T) {
		key := "METIS_DORIS_PASSWORD"
		if backend == "clickhouse" {
			key = "METIS_CLICKHOUSE_PASSWORD"
		}
		t.Setenv(key, "metis_wrong_validation_password")
		output := filepath.Join(work, "bad-credentials.json")
		cmd := exec.CommandContext(ctx, binary, "project", "validate", "--online", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--queries", filepath.Join(work, "queries.json"), "--output", output)
		stdout, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("accepted invalid database credentials")
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var report validation.Report
		if json.Unmarshal(data, &report) != nil || report.Passed || report.Complete {
			t.Fatalf("invalid failure report: %s", data)
		}
		if strings.Contains(string(data)+string(stdout), "metis_wrong_validation_password") {
			t.Fatal("credentials escaped")
		}
	})
}

type validationRegionPolicy struct{}

func (validationRegionPolicy) Evaluate(_ context.Context, request policy.Request) (policy.Decision, error) {
	decision := policy.Decision{Effect: policy.Constrained}
	for _, source := range request.Sources {
		decision.Sources = append(decision.Sources, policy.SourceConstraint{Dataset: source.Dataset, RowPredicates: []policy.Predicate{{Field: policy.FieldRef{Dataset: source.Dataset, Field: "region"}, Operator: query.FilterEQ, Values: []any{"private-policy-tenant"}}}})
	}
	return decision, nil
}
