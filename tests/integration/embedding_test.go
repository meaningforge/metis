package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIndependentHostUsesPublicModule(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	module := fmt.Sprintf("module example.com/independent-host\n\ngo 1.25.0\n\nrequire github.com/meaningforge/metis v0.0.0\nreplace github.com/meaningforge/metis => %q\n", filepath.ToSlash(root))
	fixture, err := os.ReadFile("testdata/embedhost/main.go")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"go.mod": []byte(module), "main.go": fixture} {
		if err := os.WriteFile(filepath.Join(work, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "-mod=mod", ".")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off", "GIN_MODE=release")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external module: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
