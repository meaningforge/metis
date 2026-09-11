package main

import "testing"

func TestCLIRejectsUnsupportedCommands(t *testing.T) {
	for _, command := range []string{"publish-project", "show-release", "list-releases", "restore-release"} {
		t.Run(command, func(t *testing.T) {
			if code := run([]string{command}); code != 2 {
				t.Fatalf("removed command %s returned %d", command, code)
			}
		})
	}
}
