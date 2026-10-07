package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogCommandArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"other"}, {"inspect"}, {"inspect", "--config", "x"}, {"inspect", "--config", "x", "--project", "p", "--data-source", "d", "--relations", "r", "--output", "o", "extra"}} {
		if code := runCatalog(args); code != 2 {
			t.Fatalf("%v exit=%d", args, code)
		}
	}
	if code := runCatalog([]string{"inspect", "--help"}); code != 0 {
		t.Fatalf("help exit=%d", code)
	}
}
func TestCatalogCommandDoesNotLeakConfigurationFailures(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "metis.yaml")
	if err := os.WriteFile(config, []byte("password: SUPER_SECRET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	file, err := os.CreateTemp(dir, "stderr")
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = file
	defer func() { os.Stderr = old; file.Close() }()
	code := runCatalog([]string{"inspect", "--config", config, "--project", "sales", "--data-source", "warehouse", "--relations", "missing", "--output", filepath.Join(dir, "catalog.json")})
	if code != 1 {
		t.Fatalf("exit=%d", code)
	}
	if catalogFailure(errors.New("SUPER_SECRET at private.endpoint")) != 1 {
		t.Fatal("wrong generic failure exit")
	}
	data, _ := os.ReadFile(file.Name())
	if strings.Contains(string(data), "SUPER_SECRET") || strings.Contains(string(data), "private.endpoint") {
		t.Fatalf("leaked diagnostics: %s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.json")); !os.IsNotExist(err) {
		t.Fatal("published failed snapshot")
	}
}
