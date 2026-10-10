package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestFilterNumberPrecisionAtSharedBoundary(t *testing.T) {
	for _, literal := range []string{`"0.1"`, `"9007199254740993"`, `"0.10000000000000000001"`, `["0.1","9007199254740993"]`} {
		var f Filter
		if err := json.Unmarshal([]byte(`{"value":`+literal+`}`), &f); err != nil {
			t.Fatalf("rejected %s: %v", literal, err)
		}
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var again Filter
		if err := json.Unmarshal(b, &again); err != nil || !reflect.DeepEqual(f, again) {
			t.Fatalf("roundtrip %s: %v", literal, err)
		}
	}
	for _, literal := range []string{"1e9999", "1", "true", `["1",2]`, `{"raw":"sql"}`} {
		var f Filter
		err := json.Unmarshal([]byte(`{"value":`+literal+`}`), &f)
		if err == nil {
			t.Fatalf("accepted invalid number %s", literal)
		}
		if strings.Contains(err.Error(), "9007199254740993") {
			t.Fatalf("error disclosed operand: %v", err)
		}
	}
}

func TestFilterNumberSyntaxFailureDoesNotMutateReceiver(t *testing.T) {
	f := Filter{Field: "existing", Operator: FilterEQ, Value: "old"}
	want := f
	if err := json.Unmarshal([]byte(`{"field":"new","value":["ok",1]}`), &f); err == nil {
		t.Fatal("expected syntax failure")
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatal("partial failed decode mutated filter")
	}
}
