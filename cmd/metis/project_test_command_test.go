package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/tooling/regression"
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

func TestProjectTestRejectsUnimplementedRuntimeMode(t *testing.T) {
	if code := testProject([]string{"--mode", "runtime"}); code != 2 {
		t.Fatalf("runtime mode exit = %d, want 2", code)
	}
}
