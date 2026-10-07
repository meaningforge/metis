package authoring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishingPreservesExistingPathsAndPrivatePermissions(t *testing.T) {
	s, m := exampleInputs(t, "doris")
	c, err := Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "candidate")
	if err := WriteCandidate(context.Background(), out, c); err != nil {
		t.Fatal(err)
	}
	if err := WriteCandidate(context.Background(), out, c); err == nil {
		t.Fatal("overwrote project")
	}
	for path := range c.Files {
		info, err := os.Stat(filepath.Join(out, path))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private %s: %v", path, err)
		}
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0o700 {
		t.Fatal("candidate directory not private")
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(out, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteCandidate(context.Background(), link, c); err == nil {
		t.Fatal("followed existing symlink")
	}
	stage := filepath.Join(dir, "stage")
	occupied := filepath.Join(dir, "occupied")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(occupied, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := renameExclusive(stage, occupied); err == nil {
		t.Fatal("atomic rename replaced existing empty directory")
	}
}
func TestFailedGenerationAndCancellationExposeNoPartialOutput(t *testing.T) {
	s, m := exampleInputs(t, "doris")
	c, err := Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "candidate")
	c.Files["project.yaml"] = []byte("invalid: project\n")
	if err := WriteCandidate(context.Background(), out, c); err == nil {
		t.Fatal("published invalid candidate")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("left staging files: %#v %v", entries, err)
	}
	c, err = Generate(context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	c.Files["../escape"] = []byte("bad")
	if err := WriteCandidate(context.Background(), out, c); err == nil {
		t.Fatal("allowed escaping output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Generate(ctx, s, m); err == nil {
		t.Fatal("ignored cancellation")
	}
	if err := WriteCandidate(ctx, out, c); err == nil {
		t.Fatal("ignored publishing cancellation")
	}
	entries, _ = os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".metis-authoring-") {
			t.Fatal("left partial staging")
		}
	}
}
