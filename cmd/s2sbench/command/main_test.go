package command

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestCobraRootExposesPrimaryCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"gen", "okfgen", "run", "analyze", "report", "attribution-run", "comparison-run", "stop"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help output does not contain %q:\n%s", command, stdout.String())
		}
	}
}

func TestEveryRootSubcommandUsesCobraFlagParsing(t *testing.T) {
	root := newRootCommand(io.Discard, io.Discard)
	for _, command := range root.Commands() {
		if command.DisableFlagParsing {
			t.Errorf("command %q disables Cobra flag parsing", command.Name())
		}
	}
}

func TestRunCommandPublishesCobraFlagsAndRejectsUnknownFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"run", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--suite", "--suite-path", "--arm", "--agent", "--model", "--provider", "--output", "--detach", "--resume"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("run help does not contain %q:\n%s", flag, stdout.String())
		}
	}

	root = newRootCommand(io.Discard, io.Discard)
	root.SetArgs([]string{"run", "--not-a-real-flag"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("unknown flag error = %v", err)
	}
}
