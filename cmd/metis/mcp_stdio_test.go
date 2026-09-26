package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const stdioTestConfigEnv = "METIS_TEST_STDIO_CONFIG"

func TestMain(m *testing.M) {
	if configPath := os.Getenv(stdioTestConfigEnv); configPath != "" {
		configureLogging(os.Stderr)
		if err := runStdioMCP(context.Background(), configPath, time.Second, &mcpsdk.StdioTransport{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommandLogWriterReservesStdoutForMCP(t *testing.T) {
	if got := commandLogWriter([]string{"metis", "mcp"}); got != os.Stderr {
		t.Fatalf("mcp log writer = %v, want stderr", got)
	}
	if got := commandLogWriter([]string{"metis", "serve"}); got != os.Stdout {
		t.Fatalf("serve log writer = %v, want stdout", got)
	}
}

func TestRunStdioMCPUsesProductionToolsAndLocalPrincipal(t *testing.T) {
	configPath := writeStdioTestDeployment(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runStdioMCP(ctx, configPath, time.Second, serverTransport)
	}()

	session := connectStdioTestClient(t, ctx, clientTransport)
	assertStdioToolsAndAuthorization(t, ctx, session)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("stdio server exit = %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestStdioMCPSubprocessProtocolRoundTrip(t *testing.T) {
	configPath := writeStdioTestDeployment(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable)
	command.Env = append(os.Environ(), stdioTestConfigEnv+"="+configPath)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "metis-stdio-process-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: command, TerminateDuration: time.Second}, nil)
	if err != nil {
		t.Fatalf("connect to stdio subprocess: %v; stderr=%s", err, stderr.String())
	}
	assertStdioToolsAndAuthorization(t, ctx, session)
	if err := session.Close(); err != nil {
		t.Fatalf("close stdio subprocess: %v; stderr=%s", err, stderr.String())
	}
}

func TestRunStdioMCPStopsOnCancellation(t *testing.T) {
	configPath := writeStdioTestDeployment(t)
	ctx, cancel := context.WithCancel(context.Background())
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runStdioMCP(ctx, configPath, time.Second, serverTransport)
	}()

	session := connectStdioTestClient(t, ctx, clientTransport)
	cancel()
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("cancelled stdio server exit = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stdio server did not stop after cancellation")
	}
	_ = session.Close()
}

func writeStdioTestDeployment(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeServeFile(t, filepath.Join(dir, "sales.ossie.yaml"), `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`)
	writeServeFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  sales:\n    path: ./sales.ossie.yaml\n")
	configPath := filepath.Join(dir, "metis.yaml")
	writeServeFile(t, configPath, "projects:\n  analytics:\n    path: ./project.yaml\n")
	return configPath
}

func connectStdioTestClient(t *testing.T, ctx context.Context, transport mcpsdk.Transport) *mcpsdk.ClientSession {
	t.Helper()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "metis-stdio-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func assertStdioToolsAndAuthorization(t *testing.T, ctx context.Context, session *mcpsdk.ClientSession) {
	t.Helper()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantTools := map[string]bool{
		mcp.ToolSearchOntologyConcepts: false,
		mcp.ToolResolveOntologyConcept: false,
		mcp.ToolListProjects:           false,
		mcp.ToolListModels:             false,
		mcp.ToolGetModel:               false,
		mcp.ToolListMetrics:            false,
		mcp.ToolGetMetric:              false,
		mcp.ToolGetDimensions:          false,
		mcp.ToolGetDimension:           false,
		mcp.ToolCompile:                false,
		mcp.ToolQueryMetrics:           false,
		mcp.ToolGetDimensionValues:     false,
		mcp.ToolGetRelationships:       false,
		mcp.ToolAttributeMetric:        false,
		mcp.ToolCompareMetrics:         false,
	}
	if len(tools.Tools) != len(wantTools) {
		t.Fatalf("stdio tools/list count = %d, want %d", len(tools.Tools), len(wantTools))
	}
	for _, tool := range tools.Tools {
		if _, ok := wantTools[tool.Name]; ok {
			wantTools[tool.Name] = true
		}
	}
	for name, seen := range wantTools {
		if !seen {
			t.Errorf("stdio tools/list omitted %q", name)
		}
	}

	projects, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: mcp.ToolListProjects})
	if err != nil {
		t.Fatal(err)
	}
	if projects.IsError {
		t.Fatalf("list_projects was denied for local stdio principal: %#v", projects)
	}
}
