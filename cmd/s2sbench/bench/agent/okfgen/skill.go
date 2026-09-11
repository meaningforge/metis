// Package okfgen owns the versioned Pi package used to generate OKF knowledge.
package okfgen

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed skills/google-okf-reference-agent/extension.ts
var files embed.FS

// MaterializeExtension writes the executable half of the checked-in Pi skill
// package. The caller owns cleanup; SKILL.md itself stays at its source path
// and is passed to Pi with --skill.
func MaterializeExtension() (string, func(), error) {
	root, err := os.MkdirTemp("", "metis-s2sbench-okfgen-pi-")
	if err != nil {
		return "", nil, fmt.Errorf("create Pi OKF skill extension directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	body, err := files.ReadFile("skills/google-okf-reference-agent/extension.ts")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("read embedded Pi OKF skill extension: %w", err)
	}
	path := filepath.Join(root, "extension.ts")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("materialize Pi OKF skill extension: %w", err)
	}
	return path, cleanup, nil
}
