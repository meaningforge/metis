//go:build duckdb && (darwin || linux)

package s2sbench_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

type lifecycleManifest struct {
	PID      int    `json:"pid"`
	PGID     int    `json:"pgid"`
	RunState string `json:"run_state"`
}

func TestDetachedRunAndVerifiedStopAreBlackBoxContracts(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	root := t.TempDir()
	binary := filepath.Join(root, "s2sbench")
	build := exec.Command("go", "build", "-tags", "duckdb", "-o", binary, "./cmd/s2sbench")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build s2sbench: %v\n%s", err, output)
	}
	driver := filepath.Join(root, "blocking-agent")
	if err := os.WriteFile(driver, []byte("#!/bin/sh\nset -eu\nIFS= read -r request\nsleep 60\necho '{\"protocol_version\":\"s2sbench-driver-v2\",\"output\":\"SELECT 1\",\"tool_calls\":0,\"context_tokens\":1,\"output_tokens\":1,\"transcript\":\"fake turn\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, force := range []bool{false, true} {
		name := "graceful"
		if force {
			name = "forced"
		}
		t.Run(name, func(t *testing.T) {
			outputPath := filepath.Join(root, name+" result")
			start := exec.Command(binary, "run", "--detach", "--suite", "smoke", "--scenario", "aggregation_variants", "--arm", "okf", "--agent", "generic", "--model", "model", "--provider", "provider", "--model-version", "revision", "--generic-bin", driver, "--generic-name", "fake", "--generic-version", "v1", "--output", outputPath)
			start.Dir = root
			started, err := start.CombinedOutput()
			if err != nil {
				t.Fatalf("start detached run: %v\n%s", err, started)
			}
			if !strings.Contains(string(started), "detached run is ready") {
				t.Fatalf("detached start returned before readiness metadata:\n%s", started)
			}
			control := outputPath + ".s2sbench"
			manifest := readLifecycleManifest(t, filepath.Join(control, "manifest.json"))
			if manifest.PID <= 0 || manifest.PID != manifest.PGID || manifest.RunState != "running" {
				t.Fatalf("ready manifest = %+v", manifest)
			}
			if err := syscall.Kill(manifest.PID, 0); err != nil {
				t.Fatalf("ready child is not running: %v", err)
			}
			args := []string{"stop", outputPath}
			if force {
				args = []string{"stop", "--force", filepath.Join(control, "runner.pid")}
			}
			stop := exec.Command(binary, args...)
			if stopped, err := stop.CombinedOutput(); err != nil {
				t.Fatalf("stop detached run: %v\n%s", err, stopped)
			}
			if err := syscall.Kill(manifest.PID, 0); err == nil {
				t.Fatalf("process %d survived stop", manifest.PID)
			}
			second := exec.Command(binary, "stop", filepath.Join(control, "runner.pid"))
			if stopped, err := second.CombinedOutput(); err != nil {
				t.Fatalf("idempotent stop: %v\n%s", err, stopped)
			}
		})
	}

	t.Run("generated-output", func(t *testing.T) {
		start := exec.Command(binary, "run", "--detach", "--suite", "smoke", "--scenario", "aggregation_variants", "--arm", "okf", "--agent", "generic", "--model", "model", "--provider", "provider", "--model-version", "revision", "--generic-bin", driver, "--generic-name", "fake", "--generic-version", "v1")
		start.Dir = root
		started, err := start.CombinedOutput()
		if err != nil {
			t.Fatalf("start detached run with generated output: %v\n%s", err, started)
		}
		var outputPath string
		for _, line := range strings.Split(string(started), "\n") {
			if strings.HasPrefix(line, "Run directory: ") {
				outputPath = strings.TrimPrefix(line, "Run directory: ")
			}
		}
		t.Cleanup(func() { _ = exec.Command(binary, "stop", "--force", outputPath).Run() })
		expectedParent, err := filepath.EvalSymlinks(filepath.Join(root, "s2sbench-results"))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(outputPath) != expectedParent {
			t.Fatalf("generated output = %q\n%s", outputPath, started)
		}
		stop := exec.Command(binary, "stop", "--force", outputPath)
		if stopped, err := stop.CombinedOutput(); err != nil {
			t.Fatalf("stop generated-output run: %v\n%s", err, stopped)
		}
	})
}

func readLifecycleManifest(t *testing.T, path string) lifecycleManifest {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest lifecycleManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}
