package catalog

import (
	"encoding/json"
	"testing"
)

func TestNativeEvidence(t *testing.T) {
	for _, tc := range []struct{ backend, input, want, nullable string }{
		{"doris", "decimalv3(18, 4)", `{"name":"DECIMALV3","precision":18,"scale":4}`, "unknown"},
		{"doris", "datetimev2(6)", `{"name":"DATETIMEV2","precision":6}`, "unknown"},
		{"doris", "varchar(64)", `{"name":"VARCHAR","length":64}`, "unknown"},
		{"doris", "array<int>", `{"name":"array\u003cint\u003e"}`, "unknown"},
		{"doris", "struct<Region:string>", `{"name":"struct\u003cRegion:string\u003e"}`, "unknown"},
		{"clickhouse", "Nullable(Decimal(18, 4))", `{"name":"Decimal","precision":18,"scale":4}`, "nullable"},
		{"clickhouse", "Decimal128(8)", `{"name":"Decimal","precision":38,"scale":8}`, "not_null"},
		{"clickhouse", "DateTime64(9, 'Asia/Shanghai')", `{"name":"DateTime64","precision":9,"timezone":"Asia/Shanghai"}`, "not_null"},
		{"clickhouse", "LowCardinality(Nullable(String))", `{"name":"String"}`, "nullable"},
		{"clickhouse", "Array(UInt64)", `{"name":"Array(UInt64)"}`, "not_null"},
		{"clickhouse", "FixedString(8)", `{"name":"FixedString","length":8}`, "not_null"},
		{"duckdb", "DECIMAL(18,2)", `{"name":"DECIMAL","precision":18,"scale":2}`, "unknown"},
		{"duckdb", "TIMESTAMP_NS", `{"name":"TIMESTAMP_NS","precision":9}`, "unknown"},
		{"duckdb", "TIMESTAMP WITH TIME ZONE", `{"name":"TIMESTAMP WITH TIME ZONE","precision":6}`, "unknown"},
		{"duckdb", "STRUCT(Region VARCHAR)", `{"name":"STRUCT(Region VARCHAR)"}`, "unknown"},
	} {
		t.Run(tc.backend+tc.input, func(t *testing.T) {
			parse := ParseDoris
			if tc.backend == "clickhouse" {
				parse = ParseClickHouse
			}
			if tc.backend == "duckdb" {
				parse = ParseDuckDB
			}
			typ, nullable, err := parse(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(typ)
			if string(data) != tc.want || nullable != tc.nullable {
				t.Fatalf("type=%s nullable=%s", data, nullable)
			}
		})
	}
	for _, input := range []string{"Decimal(3)", "Decimal(-1,2)", "DateTime64(nope)", "DateTime64(9, 'secret\nvalue')", ""} {
		if _, _, err := ParseClickHouse(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
