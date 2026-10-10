package doris

import (
	"encoding/json"
	"reflect"
	"testing"

	rendersql "github.com/meaningforge/metis/renderer/sql"
)

func TestDorisArgumentsPreserveExactNumbersAsDriverText(t *testing.T) {
	parameters := []rendersql.QueryParameter{
		{Value: json.Number("9007199254740993")},
		{Value: json.Number("0.10000000000000000001")},
		{Value: "ordinary"},
	}
	want := []any{"9007199254740993", "0.10000000000000000001", "ordinary"}
	if got := dorisArguments(parameters); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
	if parameters[0].Value != json.Number("9007199254740993") {
		t.Fatal("binding mutated compiled parameters")
	}
}
