package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/app/tooling/authoring"
	"go.yaml.in/yaml/v3"
)

func TestProjectInitCommandCreatesOfflineCandidates(t *testing.T) {
	for _, backend := range []string{"doris", "clickhouse"} {
		t.Run(backend, func(t *testing.T) {
			catalog := "../../examples/authoring/catalog-" + backend + ".json"
			mapping := "../../examples/authoring/model-map.yaml"
			dir := t.TempDir()
			out := filepath.Join(dir, "candidate")
			args := []string{"project", "init", "--catalog", catalog, "--mapping", mapping, "--output", out}
			if code := runOffline(args); code != 0 {
				t.Fatalf("exit=%d", code)
			}
			if result := source.ValidateProject("sales", filepath.Join(out, "project.yaml")); !result.Valid {
				t.Fatalf("validation=%#v", result)
			}
			data, err := os.ReadFile(filepath.Join(out, "authoring-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report authoring.Report
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if report.Backend != backend || report.DataSource != "warehouse" || report.Status != "candidate" || !report.Validation.Valid {
				t.Fatalf("report=%#v", report)
			}
			if bytes.Contains(data, []byte(out)) {
				t.Fatal("report includes staging/output path")
			}
			before, err := os.ReadFile(filepath.Join(out, "project.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if code := runOffline(args); code != 2 {
				t.Fatalf("existing output exit=%d", code)
			}
			after, _ := os.ReadFile(filepath.Join(out, "project.yaml"))
			if !bytes.Equal(before, after) {
				t.Fatal("overwrote candidate")
			}
			m, err := authoring.LoadMapping(mapping)
			if err != nil {
				t.Fatal(err)
			}
			m.Datasets[0].Fields[0].Column = "missing"
			badMap := filepath.Join(dir, "bad-map.yaml")
			data, err = yaml.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(badMap, data, 0o600); err != nil {
				t.Fatal(err)
			}
			failed := filepath.Join(dir, "failed")
			if code := initProject([]string{"--catalog", catalog, "--mapping", badMap, "--output", failed}); code != 1 {
				t.Fatalf("unsupported selection exit=%d", code)
			}
			if _, err := os.Stat(failed); !os.IsNotExist(err) {
				t.Fatal("exposed failed candidate")
			}
		})
	}
}

func TestProjectInitRequiresCompleteArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"--catalog", "file"}, {"--catalog", "file", "--mapping", "file", "--output", "dir", "extra"}} {
		if code := initProject(args); code != 2 {
			t.Fatalf("%v returned %d", args, code)
		}
	}
	if code := initProject([]string{"--help"}); code != 0 {
		t.Fatalf("help=%d", code)
	}
}
