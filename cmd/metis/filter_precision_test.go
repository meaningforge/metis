package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineRequestRequiresStringFilterOperands(t *testing.T) {
	decodeQueryRequest := func(data []byte) (QueryRequest, error) {
		path := filepath.Join(t.TempDir(), "request.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return LoadRequestFile(path)
	}
	for _, operand := range []string{`"9007199254740993"`, `"0.10000000000000000001"`, `["9007199254740992","9007199254740993"]`, `["0","1e-400"]`} {
		if _, err := decodeQueryRequest([]byte(`{"filters":{"kind":"filter","filter":{"field":"amount","operator":"in","value":` + operand + `}}}`)); err != nil {
			t.Fatalf("exact operand %s: %v", operand, err)
		}
	}
	for _, operand := range []string{"1", "true", `["1",2]`} {
		if _, err := decodeQueryRequest([]byte(`{"filters":{"kind":"filter","filter":{"field":"amount","operator":"eq","value":` + operand + `}}}`)); err == nil {
			t.Fatalf("accepted unsupported operand %s", operand)
		}
	}
}

func TestOfflineCompatibleDecimalReachesCompiler(t *testing.T) {
	var req QueryRequest
	if err := json.Unmarshal([]byte(`{"metrics":["total_revenue"],"filters":{"kind":"filter","filter":{"field":"total_revenue","operator":"gte","value":"0.1"}}}`), &req); err != nil {
		t.Fatal(err)
	}
	result, err := Compile(context.Background(), testDocument(), "DUCKDB", req, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parameters) != 1 || result.Parameters[0].Value != json.Number("0.1") {
		t.Fatalf("decimal binding changed: %#v", result.Parameters)
	}
}

func TestOfflineRequestRejectsLegacyFilterArray(t *testing.T) {
	var req QueryRequest
	if err := json.Unmarshal([]byte(`{"metrics":["total_revenue"],"filters":[{"field":"region","operator":"eq","value":"APAC"}]}`), &req); err == nil {
		t.Fatal("expected legacy filter array to be rejected")
	}
}
