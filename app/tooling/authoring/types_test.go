package authoring

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
)

func queryDialect(backend string) sql.SQLDialect {
	if backend == "doris" {
		return "DORIS"
	}
	if backend == "duckdb" {
		return "DUCKDB"
	}
	return "CLICKHOUSE"
}

func TestCatalogAndMappingStrictInput(t *testing.T) {
	s, m := exampleInputs(t, "doris")
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSnapshot(data); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{strings.Replace(string(data), `"nullable":"nullable"`, `"nullable":"nullable","password":"secret"`, 1), strings.Replace(string(data), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1), string(data) + "\n---\n{}", strings.Replace(string(data), `"digest":"sha256:`, `"digest":"wrong:`, 1)} {
		if _, err := ParseSnapshot([]byte(input)); err == nil {
			t.Fatal("accepted invalid catalog")
		}
	}
	md, _ := json.Marshal(m)
	if _, err := ParseMapping(md); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{string(md) + "\n---\n{}", `schema_version: 2`, strings.Replace(string(md), `"dimension":true`, `"dimension":true,"sql":"SELECT 1"`, 1), "schema_version: 1\nproject: &p sales\nmodel: *p\n"} {
		if _, err := ParseMapping([]byte(input)); err == nil {
			t.Fatal("accepted invalid map")
		}
	}
	if _, err := ParseSnapshot(append(data, []byte(strings.Repeat(" ", MaxCatalogBytes))...)); err == nil {
		t.Fatal("unbounded catalog")
	}
}

func TestNativeTypeMappingsPreserveCategoriesAndEvidence(t *testing.T) {
	precision, scale, temporal := 18, 2, 6
	utc := "UTC"
	for _, tc := range []struct {
		backend string
		native  NativeType
		want    ossie.DataType
	}{
		{"doris", NativeType{Name: "DECIMAL", Precision: &precision, Scale: &scale}, ossie.DataTypeDecimal},
		{"clickhouse", NativeType{Name: "Decimal", Precision: &precision, Scale: &scale}, ossie.DataTypeDecimal},
		{"doris", NativeType{Name: "DOUBLE"}, ossie.DataTypeFloat},
		{"clickhouse", NativeType{Name: "Float64"}, ossie.DataTypeFloat},
		{"doris", NativeType{Name: "DATEV2"}, ossie.DataTypeDate},
		{"clickhouse", NativeType{Name: "Date32"}, ossie.DataTypeDate},
		{"doris", NativeType{Name: "DATETIMEV2", Precision: &temporal}, ossie.DataTypeDateTime},
		{"clickhouse", NativeType{Name: "DateTime64", Precision: &temporal, Timezone: &utc}, ossie.DataTypeDateTimeTz},
		{"clickhouse", NativeType{Name: "DateTime64", Precision: &temporal}, ossie.DataTypeDateTime},
		{"clickhouse", NativeType{Name: "UInt64"}, ossie.DataTypeInteger},
		{"duckdb", NativeType{Name: "VARCHAR"}, ossie.DataTypeString},
		{"duckdb", NativeType{Name: "DECIMAL", Precision: &precision, Scale: &scale}, ossie.DataTypeDecimal},
		{"duckdb", NativeType{Name: "TIMESTAMP", Precision: &temporal}, ossie.DataTypeDateTime},
		{"duckdb", NativeType{Name: "TIMESTAMP WITH TIME ZONE", Precision: &temporal}, ossie.DataTypeDateTimeTz},
	} {
		got, ok := mapType(tc.backend, tc.native)
		if !ok || got != tc.want {
			t.Errorf("%#v => %s %v", tc, got, ok)
		}
	}
	bad := 100
	for _, native := range []NativeType{{Name: "JSON"}, {Name: "Array(String)"}, {Name: "Decimal"}, {Name: "Decimal", Precision: &precision, Scale: &bad}, {Name: "DateTime64", Precision: &bad}, {Name: "Nullable(Int64)"}} {
		if got, ok := mapType("clickhouse", native); ok {
			t.Errorf("coerced %#v to %s", native, got)
		}
	}
}
