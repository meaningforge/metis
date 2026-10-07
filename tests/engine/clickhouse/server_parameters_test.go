package clickhouse_test

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	clickhousebackend "github.com/meaningforge/metis/execution/backend/clickhouse"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
	testdatasource "github.com/meaningforge/metis/tests/engine/datasource"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestServerParametersRoundTripThroughProductionRunner(t *testing.T) {
	source := testdatasource.Require(t, "clickhouse")
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{"test": source.RuntimeDataSource(harness.ProductionExecutionPolicy())})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(clickhousebackend.New())
	if err != nil {
		t.Fatal(err)
	}
	execution := runner.New(sources, backends, runner.NewEnvSecretResolver(), nil)
	defer execution.Close(context.Background())
	for _, tc := range []struct {
		name     string
		value    any
		datatype ossie.DataType
		want     string
	}{
		{"quoted", "private' OR 1=1 -- ? {foreign:String}\n\\\t\x00汉字", ossie.DataTypeString, "private' OR 1=1 -- ? {foreign:String}\n\\\t\x00汉字"},
		{"null_marker_string", `\N`, ossie.DataTypeString, `\N`},
		{"empty_string", "", ossie.DataTypeString, ""},
		{"literal_null", "NULL", ossie.DataTypeString, "NULL"},
		{"large_integer", int64(9007199254740993), ossie.DataTypeInteger, "9007199254740993"},
		{"decimal", json.Number("10.2500"), ossie.DataTypeDecimal, "10.2500"},
		{"boolean", true, ossie.DataTypeBoolean, "true"},
		{"null", nil, ossie.DataTypeString, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := &artifact.CompiledQuery{SqlRenderResult: sql.SqlRenderResult{Dialect: "CLICKHOUSE", SQL: "SELECT ? AS value", Parameters: []sql.QueryParameter{{Value: tc.value}}}, OutputSchema: artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "value", Kind: artifact.OutputMetric, Datatype: tc.datatype}}}}
			result, err := execution.Execute(context.Background(), "test", compiled, runner.ExecutionOptions{})
			if err != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
				t.Fatalf("roundtrip failed: %v", err)
			}
			actual := result.Rows[0][0]
			if tc.value == nil {
				if actual != nil {
					t.Fatal("lost SQL NULL")
				}
				return
			}
			switch tc.datatype {
			case ossie.DataTypeString:
				if actual != tc.want {
					t.Fatal("string changed during server binding")
				}
			case ossie.DataTypeBoolean:
				if actual != true {
					t.Fatal("boolean changed during server binding")
				}
			case ossie.DataTypeInteger:
				if number, ok := actual.(json.Number); !ok || string(number) != tc.want {
					t.Fatal("integer lost precision")
				}
			case ossie.DataTypeDecimal:
				number, ok := actual.(string)
				if !ok {
					t.Fatalf("decimal result type %T", actual)
				}
				observed, valid := new(big.Rat).SetString(number)
				expected, _ := new(big.Rat).SetString(tc.want)
				if !valid || observed.Cmp(expected) != 0 {
					t.Fatal("decimal lost precision")
				}
			}
		})
	}
}
