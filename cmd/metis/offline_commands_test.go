package main

import (
	"os"
	"testing"
)

func TestSemanticCLIRejectsProjectGroupAndBenchmarkCommands(t *testing.T) {
	for _, subcommand := range []string{"init", "validate", "inspect", "diff", "test", "--help"} {
		if code := runOffline([]string{"project", subcommand}); code != 2 {
			t.Fatalf("retired project %s returned %d", subcommand, code)
		}
	}
	for _, subcommand := range []string{"gen", "okfgen", "run", "analyze", "report", "attribution-run", "comparison-smoke", "comparison-run", "stop"} {
		if code := runOffline([]string{"semantic", subcommand}); code != 2 {
			t.Fatalf("benchmark command semantic %s returned %d", subcommand, code)
		}
	}
	if got := commandLogWriter([]string{"metis", "semantic"}); got != os.Stderr {
		t.Fatal("semantic command logging contaminates machine-readable stdout")
	}
}

func TestOfflineCLIRejectsRetiredCommands(t *testing.T) {
	for _, args := range [][]string{
		{"query", "gen-sql"},
		{"model", "validate-model"},
		{"project", "validate-project"},
		{"project", "publish-project"},
	} {
		if code := runOffline(args); code != 2 {
			t.Fatalf("retired command %v returned %d", args, code)
		}
	}
}

func TestOfflineCLIRequiresSubcommand(t *testing.T) {
	for _, command := range []string{"query", "model", "semantic"} {
		if code := runOffline([]string{command}); code != 2 {
			t.Fatalf("command %s without subcommand returned %d", command, code)
		}
		if code := runOffline([]string{command, "--help"}); code != 0 {
			t.Fatalf("command %s help returned %d", command, code)
		}
	}
}

func TestOfflineCLICommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"query", "compile", "--help"},
		{"model", "validate", "--help"},
		{"model", "inspect", "--help"},
		{"model", "format", "--help"},
		{"semantic", "validate", "--help"},
		{"semantic", "inspect", "--help"},
		{"semantic", "diff", "--help"},
		{"semantic", "test", "--help"},
		{"semantic", "init", "--help"},
	} {
		if code := runOffline(args); code != 0 {
			t.Fatalf("command %v help returned %d", args, code)
		}
	}
}
