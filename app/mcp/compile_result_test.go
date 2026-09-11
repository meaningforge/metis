package mcp

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCompileToolResultExposesCompletePhysicalQueryWithoutDroppingStructuredOutput(t *testing.T) {
	compiled := &artifact.CompiledQuery{
		SqlStatement: sql.SqlStatement{Dialect: "DUCKDB", SQL: "SELECT ?", Parameters: []sql.QueryParameter{{Value: 7}}},
		OutputSchema: artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "answer", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeInteger}}},
	}
	result, err := compileToolResult(compiled)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v", result.Content)
	}
	text := result.Content[0].(*sdkmcp.TextContent).Text
	if strings.Contains(strings.ToLower(text), "copy") {
		t.Fatalf("compile content must not instruct clients to copy/paste values:\n%s", text)
	}
	for _, want := range []string{
		`{"dialect":"DUCKDB","sql":"SELECT ?","parameters":[{"value":7}]}`,
		`Output schema:`,
		`{"columns":[{"name":"answer","kind":"metric","datatype":"Integer"}]}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compile content omits %q:\n%s", want, text)
		}
	}
	if result.StructuredContent != nil {
		t.Fatalf("handler must leave structured content for SDK population, got %#v", result.StructuredContent)
	}
}
