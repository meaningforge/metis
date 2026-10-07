package harness

import (
	"context"
	"encoding/json"
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
	for _, scenario := range []string{"missing_column", "type_mismatch", "parameter", "compile_failure"} {
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
			if scenario == "parameter" && backend == "doris" {
				if commandErr != nil || !report.Passed {
					t.Fatalf("bound Doris EXPLAIN: %s", data)
				}
				return
			}
			if commandErr == nil || report.Passed {
				t.Fatalf("accepted broken validation: %s", data)
			}
			if scenario == "parameter" && (report.Cases[0].Outcome != "unsupported" || report.Cases[0].CatalogChecked) {
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
}
