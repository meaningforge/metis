package main

import "testing"

func TestBoundaryMatchersRejectBothDependencyDirectionsAndRuntimePaths(t *testing.T) {
	module := "example.com/metis"
	if !isForbiddenProductionImport(module, module+"/tests/conformance/scenarios") {
		t.Fatal("production-to-test import was accepted")
	}
	if !containsForbiddenRuntimePath(`open("tests/benchmarks/case.yaml")`) {
		t.Fatal("repository-relative runtime path was accepted")
	}
	if !isForbiddenTestImport(module+"/cmd/s2sbench/", module+"/cmd/s2sbench/bench/runner") {
		t.Fatal("test-to-command-internals import was accepted")
	}
}

func TestBoundaryMatchersAllowConformanceToUseStableProductionPackages(t *testing.T) {
	module := "example.com/metis"
	if isForbiddenTestImport(module+"/cmd/s2sbench/", module+"/compiler") {
		t.Fatal("stable production import was rejected")
	}
}
