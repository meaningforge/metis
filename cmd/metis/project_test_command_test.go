package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestProjectCompileSuiteCommand(t *testing.T) {
	output := filepath.Join(t.TempDir(), "report.json")
	args := []string{
		"--mode", "compile", "--project", "demo",
		"--config", "../../examples/demo/project.yaml",
		"--suite", "../../examples/demo/checks/compile.yaml",
		"--dialect", "DORIS", "--output", output,
	}
	if code := testProject(args); code != 0 {
		t.Fatalf("passing suite exit = %d", code)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var report regression.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || report.Passed != 1 {
		t.Fatalf("report = %#v", report)
	}
	if code := testProject(args); code != 2 {
		t.Fatalf("existing report exit = %d, want 2", code)
	}
	if code := testProject(append(args, "--overwrite")); code != 0 {
		t.Fatalf("overwrite exit = %d, want 0", code)
	}
}

func TestProjectTestRequiresRuntimeArguments(t *testing.T) {
	if code := testProject([]string{"--mode", "runtime"}); code != 2 {
		t.Fatalf("runtime mode exit = %d, want 2", code)
	}
}

func TestProjectTestRejectsAliasedOutputPathsBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	args := []string{"--mode", "compile", "--project", "demo", "--config", "../../examples/demo/project.yaml", "--suite", "../../examples/demo/checks/compile.yaml", "--dialect", "DORIS", "--output", filepath.Join(dir, "report"), "--junit-output", filepath.Join(alias, "report"), "--overwrite"}
	if code := testProject(args); code != 2 {
		t.Fatalf("aliased outputs exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "report")); !os.IsNotExist(err) {
		t.Fatal("wrote ambiguous output")
	}
}

func TestRuntimeExamplesAssembleAndMatchCompileSchema(t *testing.T) {
	suite, digest, err := regression.LoadSuite("../../examples/regression/results.yaml")
	if err != nil {
		t.Fatal(err)
	}
	backends, err := defaultBackends()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []struct {
		name    string
		dialect sql.SQLDialect
	}{{"doris", "DORIS"}, {"clickhouse", "CLICKHOUSE"}} {
		r, err := bootstrap.LoadRuntime("../../examples/regression/metis-"+engine.name+".yaml", bootstrap.WithBackendRegistry(backends))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		compileSuite := suite
		compileSuite.Fixture = nil
		compileSuite.Cases = append([]regression.Case(nil), suite.Cases...)
		for i := range compileSuite.Cases {
			compileSuite.Cases[i].Operation = "compile_sql"
			compileSuite.Cases[i].Expect.RowCount, compileSuite.Cases[i].Expect.Rows = nil, nil
		}
		report, err := regression.RunCompile(context.Background(), compileSuite, digest, regression.CompileOptions{Project: "regression", Config: "../../examples/regression/project.yaml", Dialect: engine.dialect})
		if err != nil || report.Status != "passed" {
			t.Fatalf("%s report=%#v err=%v", engine.name, report, err)
		}
	}
}
