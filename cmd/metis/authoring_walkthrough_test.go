package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/tooling/authoring"
	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/ossie"
)

func TestAuthoringWalkthroughAssetsAndCompileCommands(t *testing.T) {
	example := "../../examples/authoring/live"
	selectors, err := authoring.LoadSelectors(filepath.Join(example, "relations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(selectors.Relations) != 1 || strings.Join(selectors.Relations[0].Parts, ".") != "metis_authoring_demo.orders" {
		t.Fatal("walkthrough relation identity changed")
	}
	mapping, err := authoring.LoadMapping(filepath.Join(example, "model-map.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.StarterMetrics) != 1 || mapping.StarterMetrics[0].Kind != "row_count" || len(mapping.Datasets[0].Fields) != 3 {
		t.Fatal("walkthrough map inferred business semantics")
	}
	model := filepath.Join(example, "model-reviewed.ossie.yaml")
	doc, err := ossie.NewLoader().LoadFile(model)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.SemanticModel[0].Metrics) != 2 {
		t.Fatal("missing explicit reviewed metric")
	}
	suite, _, err := regression.LoadSuite(filepath.Join(example, "results.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(example, "query.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Query regression.QueryInput `json:"query"`
	}
	if json.Unmarshal(data, &request) != nil {
		t.Fatal("invalid semantic request")
	}
	queryBytes, _ := json.Marshal(request.Query)
	suiteBytes, _ := json.Marshal(suite.Cases[0].Request.Query)
	if string(queryBytes) != string(suiteBytes) {
		t.Fatal("REST request drifted from independently reviewed suite")
	}
	for _, backend := range []string{"DORIS", "CLICKHOUSE"} {
		output := filepath.Join(t.TempDir(), "query.json")
		if code := runOffline([]string{"query", "compile", "--model", model, "--dialect", backend, "--metric", "order_rows", "--metric", "total_revenue", "--dimension", "region", "--output", output}); code != 0 {
			t.Fatalf("%s compile exit=%d", backend, code)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "SUM(") || !strings.Contains(string(data), "COUNT(") || strings.Contains(string(data), "internal_note") {
			t.Fatalf("unexpected compilation: %s", data)
		}
	}
}

func TestAuthoringWalkthroughScriptsRefuseUnsafeInvocation(t *testing.T) {
	example := "../../examples/authoring/live"
	for _, script := range []string{"prepare.sh", "request.sh"} {
		if output, err := exec.Command("bash", "-n", filepath.Join(example, script)).CombinedOutput(); err != nil {
			t.Fatalf("script syntax: %s %v", output, err)
		}
	}
	for _, tc := range []struct {
		script string
		args   []string
	}{
		{"prepare.sh", nil}, {"prepare.sh", []string{"unsupported", "unused"}},
		{"request.sh", []string{"https://remote.example"}},
		{"request.sh", []string{"http://127.0.0.1:8080@remote.example"}},
	} {
		var exit *exec.ExitError
		if err := exec.Command("bash", append([]string{filepath.Join(example, tc.script)}, tc.args...)...).Run(); !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("accepted unsafe invocation: %#v", tc)
		}
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "keep")
	if err := os.WriteFile(marker, []byte("existing user data"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", filepath.Join(example, "prepare.sh"), "doris", dir)
	command.Env = append(os.Environ(), "METIS_BIN=true")
	if err := command.Run(); err == nil {
		t.Fatal("accepted existing work directory")
	}
	data, _ := os.ReadFile(marker)
	if string(data) != "existing user data" {
		t.Fatal("modified existing work directory")
	}
}

func TestAuthoringWalkthroughRequestScript(t *testing.T) {
	request, _ := os.ReadFile("../../examples/authoring/live/query.json")
	seen := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen <- r.URL.Path == "/v1/query-metrics" && r.Header.Get("Authorization") == "Bearer walkthrough-test-key" && r.Header.Get("Content-Type") == "application/json" && string(body) == string(request)
		w.Write([]byte(`{"count":0,"rows":[]}`))
	}))
	defer server.Close()
	command := exec.Command("bash", "../../examples/authoring/live/request.sh", server.URL)
	command.Env = append(os.Environ(), "METIS_API_KEY=walkthrough-test-key")
	output, err := command.CombinedOutput()
	if err != nil || !<-seen || strings.Contains(string(output), "walkthrough-test-key") {
		t.Fatalf("request helper failed: %s %v", output, err)
	}
}
