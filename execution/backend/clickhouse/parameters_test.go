package clickhouse

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestServerBindingRewritesOnlyActualPlaceholders(t *testing.T) {
	private := "private' OR 1=1 -- ? {foreign:String}\n\\"
	original := sql.SqlRenderResult{SQL: "SELECT '?' AS literal, `?` AS quoted /* ? /* ? */ */ FROM t WHERE x = ? AND y = ? -- ?\n", Parameters: []sql.QueryParameter{{Value: private}, {Value: int64(9007199254740993)}}}
	text, parameters, err := bindServerParameters(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, private) || !strings.Contains(text, "x = {metis_param_0:String}") || !strings.Contains(text, "y = {metis_param_1:Int64}") || !strings.Contains(text, "'?' AS literal") || !strings.Contains(text, "/* ? /* ? */ */") {
		t.Fatalf("changed SQL shape: %s", text)
	}
	if parameters["metis_param_0"] != escapeParameterString(private) || parameters["metis_param_1"] != "9007199254740993" {
		t.Fatal("changed parameter values")
	}
	if original.Parameters[0].Value != private || strings.Contains(original.SQL, "metis_param") {
		t.Fatal("mutated compiler artifact")
	}
}

func TestServerParameterClosedDomain(t *testing.T) {
	for _, tc := range []struct {
		value      any
		kind, text string
	}{
		{nil, "Nullable(String)", `\N`}, {true, "Bool", "true"}, {[]byte{0, 255}, "String", "\\0" + string([]byte{255})},
		{json.Number("10.2500"), "Decimal(76,4)", "10.2500"}, {json.Number("9007199254740993"), "Int64", "9007199254740993"},
		{json.Number("12345678901234567890123456789012345678"), "Decimal(76,0)", "12345678901234567890123456789012345678"},
		{json.Number("0.10000000000000000001"), "Decimal(76,20)", "0.10000000000000000001"},
		{"2026-01-01T12:34:56.123456Z", "String", "2026-01-01T12:34:56.123456Z"},
	} {
		kind, text, err := serverParameter(tc.value)
		if err != nil || kind != tc.kind || text != tc.text {
			t.Fatalf("value=%v kind=%s text=%q err=%v", tc.value, kind, text, err)
		}
	}
	for _, value := range []any{math.NaN(), math.Inf(1), json.Number("1e200"), json.Number("invalid"), []any{1}, map[string]int{"a": 1}} {
		if _, _, err := serverParameter(value); err == nil {
			t.Fatalf("accepted unsupported value: %v", value)
		}
	}
}

func TestEscapedStringProtocolPreservesControlsAndNullMarker(t *testing.T) {
	if actual := escapeParameterString("\\N\t\n\r\x00"); actual != "\\\\N\\t\\n\\r\\0" {
		t.Fatalf("transport encoding=%q", actual)
	}
}

func TestUnsupportedBindingPreflight(t *testing.T) {
	for _, text := range []string{"SELECT '?'", "SELECT ? + ?", "SELECT 'unterminated ?", "SELECT ? /* unterminated", "SELECT {foreign:String}, ?"} {
		compiled := &artifact.CompiledQuery{SqlRenderResult: sql.SqlRenderResult{SQL: text, Parameters: []sql.QueryParameter{{Value: "private"}}}}
		before := compiled.SqlRenderResult
		if NewDriverFactory().SupportsCompiledValidation(compiled) {
			t.Fatalf("accepted unsafe shape: %s", text)
		}
		if !reflect.DeepEqual(before, compiled.SqlRenderResult) {
			t.Fatal("preflight mutated artifact")
		}
	}
}
