package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/httperr"
	"github.com/meaningforge/metis/app/tooling/authoring"
	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/app/tooling/validation"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
	testdatasource "github.com/meaningforge/metis/tests/engine/datasource"
)

func authoringExample(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../../examples/authoring/live")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// AuthoringSetupStatements reads the exact SQL supplied to human users. The
// fixture owns a fresh disposable database and is not executed by Metis tooling.
func AuthoringSetupStatements(t *testing.T, backend string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(authoringExample(t), "setup-"+backend+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	for _, statement := range strings.Split(string(data), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			result = append(result, statement)
		}
	}
	return result
}

// RunAuthoringWalkthrough executes the checked-in walkthrough through the real
// executable and HTTP server. It does not reimplement catalog or generation APIs.
func RunAuthoringWalkthrough(t *testing.T, backend, username, password string) {
	t.Helper()
	example := authoringExample(t)
	repo := filepath.Clean(filepath.Join(example, "../../.."))
	source := testdatasource.Require(t, backend)
	if username == "" || password == "" {
		t.Fatal("walkthrough requires a nonempty test identity and password")
	}
	if backend == "doris" {
		t.Setenv("METIS_DORIS_HOST", source.RequireValue(t, "host"))
		t.Setenv("METIS_DORIS_PORT", source.RequireValue(t, "port"))
		t.Setenv("METIS_DORIS_USERNAME", username)
		t.Setenv("METIS_DORIS_PASSWORD", password)
	} else {
		t.Setenv("METIS_CLICKHOUSE_ADDRESS", source.RequireValue(t, "address"))
		t.Setenv("METIS_CLICKHOUSE_USERNAME", username)
		t.Setenv("METIS_CLICKHOUSE_PASSWORD", password)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "metis")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := func(name string, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = repo
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("walkthrough command %s %v failed: %s", filepath.Base(name), args, output)
		}
		return output
	}
	run("go", "build", "-o", binary, "./cmd/metis")
	t.Setenv("METIS_BIN", binary)
	work := filepath.Join(dir, "work")
	run("bash", filepath.Join(example, "prepare.sh"), backend, work)
	snapshot, err := authoring.LoadSnapshot(filepath.Join(work, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Relations) != 1 || len(snapshot.Relations[0].Columns) != 4 {
		t.Fatal("walkthrough catalog inventory changed")
	}
	generated := filepath.Join(work, "candidate-sales/models/sales.ossie.yaml")
	doc, err := ossie.NewLoader().LoadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.SemanticModel[0].Metrics) != 1 || doc.SemanticModel[0].Metrics[0].Name != "order_rows" {
		t.Fatal("generation inferred a business metric")
	}
	for _, field := range doc.SemanticModel[0].Datasets[0].Fields {
		if strings.Contains(field.Expression.Dialects[0].Expression, "internal_note") {
			t.Fatal("published unselected field")
		}
	}
	// Explicit adoption happens only in this fresh test-owned candidate, after
	// verifying that the generator did not infer the business definition.
	reviewed, err := os.ReadFile(filepath.Join(work, "model-reviewed.ossie.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, reviewed, 0600); err != nil {
		t.Fatal(err)
	}
	run(binary, "project", "validate", "--project", "sales", "--config", filepath.Join(work, "candidate-sales/project.yaml"))
	run(binary, "query", "compile", "--model", generated, "--dialect", strings.ToUpper(backend), "--metric", "order_rows", "--metric", "total_revenue", "--dimension", "region", "--output", filepath.Join(work, "reviewed-query.json"))
	run(binary, "project", "validate", "--online", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--queries", filepath.Join(work, "queries.json"), "--output", filepath.Join(work, "validation.json"))
	validationData, err := os.ReadFile(filepath.Join(work, "validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var validated validation.Report
	if err := json.Unmarshal(validationData, &validated); err != nil || !validated.Passed || !validated.Complete || len(validated.Cases) != 1 || !validated.Cases[0].EnginePrepared {
		t.Fatalf("online validation failed: %s", validationData)
	}
	checkOnlineValidationFailures(t, ctx, binary, work, generated, reviewed, backend)
	run(binary, "project", "test", "--mode", "runtime", "--project", "sales", "--config", filepath.Join(work, "metis.yaml"), "--suite", filepath.Join(work, "results.yaml"), "--output", filepath.Join(work, "results.json"))
	data, err := os.ReadFile(filepath.Join(work, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report regression.Report
	if err := json.Unmarshal(data, &report); err != nil || report.Status != "passed" || report.Passed != 1 {
		t.Fatalf("walkthrough suite did not pass: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	t.Setenv("METIS_API_KEY", "metis-authoring-walkthrough-test-token")
	server := exec.CommandContext(ctx, binary, "serve", "--config", filepath.Join(work, "metis.yaml"), "--addr", address, "--shutdown-timeout", "3s")
	server.Dir = repo
	log, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	defer func() {
		server.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			server.Process.Kill()
			<-done
		}
		log.Close()
	}()
	client := &http.Client{Timeout: time.Second}
	base := "http://" + address
	ready := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(base + "/readyz")
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("walkthrough server did not become ready")
	}
	response, err := client.Post(base+"/v1/query-metrics", "application/json", bytes.NewReader([]byte(`{"query":{"project":"sales","model":"sales"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != httperr.StatusOf(serrors.ErrUnauthenticated) {
		t.Fatalf("unauthenticated query status=%d", response.StatusCode)
	}
	output := run("bash", filepath.Join(example, "request.sh"), base)
	var result struct {
		Count  int64               `json:"count"`
		Rows   [][]json.RawMessage `json:"rows"`
		Schema struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"schema"`
	}
	if json.Unmarshal(output, &result) != nil || result.Count != 2 || len(result.Rows) != 2 || len(result.Schema.Columns) != 3 {
		t.Fatalf("unexpected walkthrough result: %s", output)
	}
	for i, name := range []string{"region", "order_rows", "total_revenue"} {
		if result.Schema.Columns[i].Name != name {
			t.Fatalf("wrong result schema: %s", output)
		}
	}
	seen := map[string]bool{}
	for _, row := range result.Rows {
		if len(row) != 3 {
			t.Fatal("wrong result row width")
		}
		var region, revenue string
		var count int64
		if json.Unmarshal(row[0], &region) != nil || json.Unmarshal(row[1], &count) != nil || json.Unmarshal(row[2], &revenue) != nil || seen[region] {
			t.Fatal("unexpected result row")
		}
		seen[region] = true
		wantCount, wantRevenue := int64(0), ""
		switch region {
		case "APAC":
			wantCount, wantRevenue = 2, "10.25"
		case "EMEA":
			wantCount, wantRevenue = 1, "7.50"
		default:
			t.Fatal("unexpected region")
		}
		actual, ok := new(big.Rat).SetString(revenue)
		want, _ := new(big.Rat).SetString(wantRevenue)
		if !ok || count != wantCount || actual.Cmp(want) != 0 {
			t.Fatalf("wrong demo result for %s", region)
		}
	}
}
