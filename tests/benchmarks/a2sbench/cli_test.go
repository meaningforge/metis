package a2sbench_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuiltCLIWorksOutsideRepository(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	external := t.TempDir()
	binary := filepath.Join(external, "a2sbench")
	build := exec.Command("go", "build", "-o", binary, "./cmd/a2sbench")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build a2sbench: %v\n%s", err, output)
	}

	command := exec.Command(binary, "--help")
	command.Dir = external
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run copied a2sbench: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "a2sbench [command]") || !strings.Contains(string(output), "Agent-to-SQL") || !strings.Contains(string(output), "Metis Core") || !strings.Contains(string(output), "controlled baselines") {
		t.Fatalf("missing Metis Core benchmark purpose: %s", output)
	}
	for _, name := range []string{"semantic", "catalog", "serve"} {
		command := exec.Command(binary, name)
		command.Dir = external
		if output, err := command.CombinedOutput(); err == nil || !strings.Contains(string(output), "unknown command") {
			t.Fatalf("benchmark accepted semantic-layer command %s: %v %s", name, err, output)
		}
	}
	for _, name := range []string{"gen", "okfgen", "run", "analyze", "report", "attribution-run", "comparison-run", "stop"} {
		if !strings.Contains(string(output), name) {
			t.Errorf("help omits %q:\n%s", name, output)
		}
	}
}

func TestBuiltCLIReportsMissingAgentBeforeBenchmarkExecution(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	external := t.TempDir()
	binary := filepath.Join(external, "a2sbench")
	build := exec.Command("go", "build", "-o", binary, "./cmd/a2sbench")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build a2sbench: %v\n%s", err, output)
	}
	command := exec.Command(binary, "run", "--suite", "smoke", "--arm", "okf", "--agent", "generic", "--model", "model", "--provider", "provider", "--model-version", "revision", "--generic-bin", filepath.Join(external, "missing-agent"), "--generic-name", "missing", "--generic-version", "v1", "--output", filepath.Join(external, "result"))
	command.Dir = external
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("missing Agent unexpectedly started:\n%s", output)
	}
	if !strings.Contains(string(output), "not usable") {
		t.Fatalf("missing Agent error is not actionable:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(external, "result.partial")); !os.IsNotExist(statErr) {
		t.Fatalf("benchmark execution mutated partial results before preflight: %v", statErr)
	}
}
