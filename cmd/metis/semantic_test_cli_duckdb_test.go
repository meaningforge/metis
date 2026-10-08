//go:build duckdb

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	duckdbnative "github.com/duckdb/duckdb-go/v2"
	"github.com/meaningforge/metis/app/tooling/regression"
)

// Unlike command-handler tests, this exercises the built executable, dispatch,
// process exit status, production read-only driver, and the shipped CI pipeline.
// Expected rows are the existing independently reviewed regression example.
func TestSemanticRegressionBuiltCLIAndCIPipeline(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	binary := filepath.Join(dir, "metis")
	build := exec.CommandContext(ctx, "go", "build", "-tags", "duckdb", "-o", binary, "./cmd/metis")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	database := filepath.Join(dir, "fixture.duckdb")
	connector, err := duckdbnative.NewConnector(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	// Fixture preparation is independent of the CLI and never part of the suite.
	_, setupErr := db.Exec(`CREATE SCHEMA metis_regression;
CREATE TABLE metis_regression.orders(region VARCHAR NOT NULL, amount DECIMAL(18,2) NOT NULL);
INSERT INTO metis_regression.orders VALUES ('APAC',50), ('APAC',70), ('EMEA',80);`)
	var sum, average float64
	if setupErr == nil {
		setupErr = db.QueryRow("SELECT SUM(amount)::DOUBLE, AVG(amount)::DOUBLE FROM metis_regression.orders WHERE region = 'APAC'").Scan(&sum, &average)
	}
	closeErr := db.Close()
	if setupErr != nil || closeErr != nil {
		t.Fatalf("fixture setup=%v close=%v", setupErr, closeErr)
	}
	if sum != 120 || average != 60 {
		t.Fatalf("fixture must distinguish SUM 120 from AVG 60, got %v/%v", sum, average)
	}
	copyExample := func(name string) []byte {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, "examples/regression", name))
		if err != nil {
			t.Fatal(err)
		}
		writeServeFile(t, filepath.Join(dir, name), string(body))
		return body
	}
	model := copyExample("sales.ossie.yaml")
	copyExample("project.yaml")
	copyExample("results.yaml")
	writeServeFile(t, filepath.Join(dir, "datasources.yaml"), "local:\n  type: duckdb\n  config:\n    path: "+database+"\n  policy:\n    query_timeout: 5s\n    max_rows: 10\n    max_bytes: 1024\n")
	writeServeFile(t, filepath.Join(dir, "metis.yaml"), "projects:\n  regression: {path: ./project.yaml, data_source: local}\ndata_sources: {path: ./datasources.yaml}\n")
	suite, _, err := regression.LoadSuite(filepath.Join(dir, "results.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	suite.Fixture = nil
	suite.Cases[0].Operation = "compile_sql"
	suite.Cases[0].Expect.Rows, suite.Cases[0].Expect.RowCount = nil, nil
	compileInput, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	writeServeFile(t, filepath.Join(dir, "compile.json"), string(compileInput))
	writeServeFile(t, filepath.Join(dir, "invalid.yaml"), "schema_version: 1\nunknown: true\n")
	type counts struct{ passed, failed, notRun int }
	cases := []struct {
		name, mode, config, suite string
		exit                      int
		want                      *counts
		regression, blocked       bool
	}{
		{"compile_pass", "compile", "project.yaml", "compile.json", 0, &counts{passed: 1}, false, false},
		{"runtime_pass", "runtime", "metis.yaml", "results.yaml", 0, &counts{passed: 1}, false, false},
		{"compile_sum_to_avg", "compile", "project.yaml", "compile.json", 0, &counts{passed: 1}, true, false},
		{"runtime_sum_to_avg", "runtime", "metis.yaml", "results.yaml", 1, &counts{failed: 1}, true, false},
		{"missing_configuration", "runtime", "absent.yaml", "results.yaml", 1, &counts{notRun: 1}, false, false},
		{"invalid_suite", "compile", "project.yaml", "invalid.yaml", 2, nil, false, false},
		{"blocked_report", "compile", "project.yaml", "compile.json", 2, nil, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := string(model)
			if tc.regression {
				candidate = strings.Replace(candidate, "SUM(orders.amount)", "AVG(orders.amount)", 1)
				if candidate == string(model) {
					t.Fatal("regression mutation did not change the model")
				}
			}
			writeServeFile(t, filepath.Join(dir, "sales.ossie.yaml"), candidate)
			output, junit := filepath.Join(dir, tc.name+".json"), filepath.Join(dir, tc.name+".xml")
			if tc.blocked {
				// A file as the report's parent is deterministically unwritable even
				// when tests run as root; chmod-based tests are not portable.
				parent := filepath.Join(dir, "not-a-directory")
				writeServeFile(t, parent, "keep me")
				output = filepath.Join(parent, "report.json")
			}
			args := []string{"--mode", tc.mode, "--project", "regression", "--config", filepath.Join(dir, tc.config), "--suite", filepath.Join(dir, tc.suite), "--output", output, "--junit-output", junit}
			if tc.mode == "compile" {
				args = append(args, "--dialect", "DUCKDB")
			}
			command := exec.CommandContext(ctx, "bash", append([]string{filepath.Join(root, "examples/regression/run-ci.sh")}, args...)...)
			command.Env = append(os.Environ(), "METIS_BIN="+binary, "METIS_CI_LOG="+filepath.Join(dir, tc.name+".log"))
			result, err := command.CombinedOutput()
			exit := 0
			if err != nil {
				if failure, ok := err.(*exec.ExitError); ok {
					exit = failure.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != tc.exit {
				t.Fatalf("pipeline exit=%d want=%d: %s", exit, tc.exit, result)
			}
			if tc.want == nil {
				if _, err := os.Stat(junit); !os.IsNotExist(err) {
					t.Fatal("invalid input produced a misleading JUnit report")
				}
				if !tc.blocked {
					if _, err := os.Stat(output); !os.IsNotExist(err) {
						t.Fatal("invalid suite produced a misleading JSON report")
					}
				} else if body, err := os.ReadFile(filepath.Dir(output)); err != nil || string(body) != "keep me" {
					t.Fatal("blocked report damaged the existing file")
				}
				return
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal("JSON report not preserved:", err)
			}
			var report regression.Report
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if report.Passed != tc.want.passed || report.Failed != tc.want.failed || report.NotRun != tc.want.notRun || len(report.Cases) != 1 || (report.Status == "passed") != (exit == 0) {
				t.Fatalf("exit=%d report=%#v", exit, report)
			}
			if tc.regression && tc.mode == "runtime" && (report.Cases[0].ActualOutcome != "success" || report.Cases[0].Category != "runtime_assertion_mismatch" || len(report.Cases[0].Differences) == 0) {
				t.Fatalf("mutation must execute successfully but fail the result assertion: %#v", report)
			}
			var xmlReport struct {
				Tests    int `xml:"tests,attr"`
				Failures int `xml:"failures,attr"`
				Errors   int `xml:"errors,attr"`
				Cases    []struct {
					Failure *struct{} `xml:"failure"`
					Error   *struct{} `xml:"error"`
					Skipped *struct{} `xml:"skipped"`
				} `xml:"testcase"`
			}
			data, err = os.ReadFile(junit)
			if err != nil || xml.Unmarshal(data, &xmlReport) != nil {
				t.Fatalf("JUnit report not preserved or invalid: %v", err)
			}
			if xmlReport.Tests != 1 || xmlReport.Failures != report.Failed || xmlReport.Errors != report.NotRun || len(xmlReport.Cases) != 1 || xmlReport.Cases[0].Skipped != nil || (xmlReport.Cases[0].Failure != nil) != (report.Failed == 1) || (xmlReport.Cases[0].Error != nil) != (report.NotRun == 1) {
				t.Fatalf("inconsistent JUnit=%#v JSON=%#v", xmlReport, report)
			}
		})
	}
}
