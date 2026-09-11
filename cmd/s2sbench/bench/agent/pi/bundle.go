// Package pi owns the embedded Pi S2SBench protocol adapter.
package pi

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed driver.mjs metis.ts
var bundle embed.FS

// Materialize writes the adapter beside its MCP extension because driver.mjs
// resolves metis.ts relative to itself. The caller owns cleanup.
func Materialize() (string, func(), error) {
	root, err := os.MkdirTemp("", "metis-s2sbench-pi-")
	if err != nil {
		return "", nil, fmt.Errorf("create Pi adapter directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	for _, name := range []string{"driver.mjs", "metis.ts"} {
		body, readErr := bundle.ReadFile(name)
		if readErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("read embedded Pi adapter %q: %w", name, readErr)
		}
		mode := os.FileMode(0o600)
		if name == "driver.mjs" {
			mode = 0o700
		}
		if writeErr := os.WriteFile(filepath.Join(root, name), body, mode); writeErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("materialize Pi adapter %q: %w", name, writeErr)
		}
	}
	return filepath.Join(root, "driver.mjs"), cleanup, nil
}
