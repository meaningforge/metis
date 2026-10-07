package main

import "testing"

func TestProjectValidationModeArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--online", "--offline", "--project", "sales", "--config", "missing"},
		{"--online", "--project", "sales", "--config", "missing"},
		{"--offline", "--project", "sales", "--config", "missing", "--queries", "missing"},
		{"--project", "sales", "--config", "missing", "unexpected"},
	} {
		if code := validateProject(args); code != 2 {
			t.Fatalf("args=%v exit=%d", args, code)
		}
	}
}
