package main

import "testing"

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
	for _, command := range []string{"query", "model", "project"} {
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
		{"project", "validate", "--help"},
		{"project", "inspect", "--help"},
		{"project", "diff", "--help"},
	} {
		if code := runOffline(args); code != 0 {
			t.Fatalf("command %v help returned %d", args, code)
		}
	}
}
