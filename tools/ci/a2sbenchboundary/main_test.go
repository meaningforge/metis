package main

import "testing"

func TestMetisAuthoringCannotImportBenchmarkImplementation(t *testing.T) {
	module := "example.com/metis"
	for _, owner := range []string{module + "/cmd/metis", module + "/app/tooling/regression", module + "/app/tooling/authoring"} {
		if !isForbiddenMetisToolingImport(module, owner, module+"/cmd/a2sbench/bench/runner") {
			t.Fatalf("benchmark dependency accepted for %s", owner)
		}
		if isForbiddenMetisToolingImport(module, owner, module+"/app/service/semantic") {
			t.Fatalf("shared semantic service rejected for %s", owner)
		}
	}
}

func TestBoundaryMatchersRejectBothDependencyDirectionsAndRuntimePaths(t *testing.T) {
	module := "example.com/metis"
	if !isForbiddenProductionImport(module, module+"/tests/conformance/scenarios") {
		t.Fatal("production-to-test import was accepted")
	}
	if !containsForbiddenRuntimePath(`open("tests/benchmarks/case.yaml")`) {
		t.Fatal("repository-relative runtime path was accepted")
	}
	if !isForbiddenTestImport(module+"/cmd/a2sbench/", module+"/cmd/a2sbench/bench/runner") {
		t.Fatal("test-to-command-internals import was accepted")
	}
}

func TestBoundaryMatchersAllowConformanceToUseStableProductionPackages(t *testing.T) {
	module := "example.com/metis"
	if isForbiddenTestImport(module+"/cmd/a2sbench/", module+"/compiler") {
		t.Fatal("stable production import was rejected")
	}
}
