//go:build duckdb

package duckdb_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	duckdbnative "github.com/duckdb/duckdb-go/v2"
	"github.com/meaningforge/metis/app/tooling/authoring"
	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/app/tooling/validation"
)

// Runs the shipped tutorial with writable setup independent of Metis. Every
// CLI database operation uses the production read-only backend.
func TestDuckDBAuthoringWalkthroughThroughCLI(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	example := filepath.Join(root, "examples/authoring/duckdb")
	dir := t.TempDir()
	database := filepath.Join(dir, "demo.duckdb")
	connector, err := duckdbnative.NewConnector(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	setup, err := os.ReadFile(filepath.Join(example, "setup.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(setup)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	binary := filepath.Join(dir, "metis")
	run := func(name string, args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, name, args...)
		command.Dir = root
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("tutorial %s %v: %s", filepath.Base(name), args, result)
		}
		return result
	}
	run("go", "build", "-tags", "duckdb", "-o", binary, "./cmd/metis")
	t.Setenv("METIS_BIN", binary)
	t.Setenv("METIS_DUCKDB_PATH", database)
	work := filepath.Join(dir, "work")
	run("bash", filepath.Join(example, "prepare.sh"), database, work)
	snapshot, err := authoring.LoadSnapshot(filepath.Join(work, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Backend != "duckdb" || len(snapshot.Relations) != 1 || len(snapshot.Relations[0].Columns) != 4 {
		t.Fatal("incomplete DuckDB snapshot")
	}
	model := filepath.Join(work, "candidate-sales/models/sales.ossie.yaml")
	body, err := os.ReadFile(model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "internal_note") || strings.Contains(string(body), "total_revenue") {
		t.Fatal("generator inferred or included unselected semantics")
	}
	run(binary, "semantic", "validate", "--offline", "--project", "sales", "--config", filepath.Join(work, "candidate-sales/project.yaml"))
	reviewed, err := os.ReadFile(filepath.Join(work, "model-reviewed.ossie.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, reviewed, 0600); err != nil {
		t.Fatal(err)
	}
	for _, inventory := range []string{"queries.json", "queries-filtered.json"} {
		output := filepath.Join(work, inventory+"-report.json")
		run(binary, "semantic", "validate", "--online", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--queries", filepath.Join(work, inventory), "--output", output)
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var report validation.Report
		if json.Unmarshal(data, &report) != nil || !report.Complete || !report.Passed || !report.Cases[0].EnginePrepared {
			t.Fatalf("online report=%s", data)
		}
	}
	run(binary, "semantic", "test", "--mode", "runtime", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--suite", filepath.Join(work, "results.yaml"), "--output", filepath.Join(work, "results.json"))
	data, err := os.ReadFile(filepath.Join(work, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report regression.Report
	if json.Unmarshal(data, &report) != nil || report.Status != "passed" || report.Passed != 1 {
		t.Fatalf("independent results=%s", data)
	}
	// Deliberately broken physical dependencies cannot produce partial success.
	if err := os.WriteFile(model, []byte(strings.Replace(string(reviewed), "expression: orders.amount", "expression: orders.absent_amount", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(work, "missing-column.json")
	args := []string{"semantic", "validate", "--online", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--queries", filepath.Join(work, "queries.json"), "--output", output}
	if _, err := exec.CommandContext(ctx, binary, args...).CombinedOutput(); err == nil {
		t.Fatal("accepted missing physical column")
	}
	data, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var failure validation.Report
	if json.Unmarshal(data, &failure) != nil || failure.Passed || failure.Cases[0].Code != "source_dependency_mismatch" || failure.Cases[0].EnginePrepared {
		t.Fatalf("missing column=%s", data)
	}
	before, _ := os.ReadFile(output)
	if _, err := exec.CommandContext(ctx, binary, args...).CombinedOutput(); err == nil {
		t.Fatal("overwrote report")
	}
	after, _ := os.ReadFile(output)
	if string(before) != string(after) {
		t.Fatal("report changed")
	}
}
