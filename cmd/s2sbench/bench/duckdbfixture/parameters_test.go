//go:build duckdb

package fixture

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/renderer/sql"
)

func TestRunSQLBindsValuesWithoutInterpolation(t *testing.T) {
	ctx := context.Background()
	backend, err := New(filepath.Join(t.TempDir(), "bindings.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close(ctx)
	if err := backend.Execute(ctx, "CREATE TABLE sentinel (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"O'Reilly", `C:\path\?`, "a\nb\x00c", "中文 ? -- /* */", "'); DROP TABLE sentinel; --"} {
		result, err := backend.RunSQL(ctx, "SELECT CAST(? AS VARCHAR) AS value", sql.QueryParameter{Value: value})
		if err != nil {
			t.Fatalf("bind %q: %v", value, err)
		}
		if len(result.Rows) != 1 || result.Rows[0][0].Canonical != value {
			t.Fatalf("round trip %q: %#v", value, result)
		}
	}
	result, err := backend.RunSQL(ctx, "SELECT CAST(? AS BIGINT) AS value", sql.QueryParameter{Value: json.Number("9007199254740993")})
	if err != nil || result.Rows[0][0].Canonical != "9007199254740993" {
		t.Fatalf("integer precision: %#v, %v", result, err)
	}
	if _, err := backend.RunSQL(ctx, "SELECT * FROM sentinel"); err != nil {
		t.Fatal(err)
	}
	for _, params := range [][]sql.QueryParameter{nil, {{Value: 1}, {Value: 2}}} {
		if _, err := backend.RunSQL(ctx, "SELECT CAST(? AS BIGINT) AS value", params...); err == nil {
			t.Fatalf("driver accepted mismatched bindings: %#v", params)
		}
	}
}
