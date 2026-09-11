package s2sbench_test

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
	binary := filepath.Join(external, "s2sbench")
	build := exec.Command("go", "build", "-o", binary, "./cmd/s2sbench")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build s2sbench: %v\n%s", err, output)
	}

	command := exec.Command(binary, "--help")
	command.Dir = external
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run copied s2sbench: %v\n%s", err, output)
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
	binary := filepath.Join(external, "s2sbench")
	build := exec.Command("go", "build", "-o", binary, "./cmd/s2sbench")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build s2sbench: %v\n%s", err, output)
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
