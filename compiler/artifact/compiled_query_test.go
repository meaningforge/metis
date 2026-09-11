package artifact

import (
	"testing"

	"github.com/meaningforge/metis/renderer/sql"
)

func TestSnapshotCompiledQueryOwnsMutableArtifactContainers(t *testing.T) {
	parameter := []byte("a")
	compiled, err := NewCompiledQuery(
		sql.SQLQuery{Dialect: "DORIS", SQL: "SELECT ?", Parameters: []sql.QueryParameter{{Name: "value", Value: parameter}}},
		OutputSchema{Columns: []OutputColumn{{Name: "value"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	parameter[0] = 'b'
	query := compiled.PhysicalQuery
	if string(query.Parameters[0].Value.([]byte)) != "a" {
		t.Fatalf("parameter value = %#v, want owned copy", query.Parameters[0].Value)
	}
}

func TestSnapshotCompiledQueryRejectsUnknownParameterValue(t *testing.T) {
	_, err := NewCompiledQuery(
		sql.SQLQuery{Dialect: "DORIS", SQL: "SELECT ?", Parameters: []sql.QueryParameter{{Value: &struct{}{}}}},
		OutputSchema{},
	)
	if err == nil {
		t.Fatal("expected unsupported parameter value to fail")
	}
}

func TestNewCompiledQueryRejectsStructurallyInvalidPhysicalQuery(t *testing.T) {
	for _, query := range []sql.SQLQuery{
		{SQL: "SELECT 1"},
		{Dialect: "DORIS", SQL: " \t\n"},
	} {
		if _, err := NewCompiledQuery(query, OutputSchema{}); err == nil {
			t.Fatalf("NewCompiledQuery(%#v) succeeded", query)
		}
	}
}
