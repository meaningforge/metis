package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestFilterNumberPrecisionAtSharedBoundary(t *testing.T) {
	for _, literal := range []string{"0.1", "1.25", "1e3", "9007199254740993", "1000000000000000100", "0.10000000000000000001", "1e400", "1e-400", "-0", "0e-999", "[0.1,9007199254740993]", `"9007199254740993"`, "true", "null"} {
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
	for _, literal := range []string{"1e9999", "+1", "01", ".1", "NaN"} {
		var f Filter
		err := json.Unmarshal([]byte(`{"value":`+literal+`}`), &f)
		if err == nil {
			t.Fatalf("accepted invalid number %s", literal)
		}
		if strings.Contains(err.Error(), literal) {
			t.Fatalf("error disclosed operand: %v", err)
		}
	}
}

func TestFilterNumberSyntaxFailureDoesNotMutateReceiver(t *testing.T) {
	f := Filter{Field: "existing", Operator: FilterEQ, Value: "old"}
	want := f
	if err := json.Unmarshal([]byte(`{"field":"new","value":[1,1e9999]}`), &f); err == nil {
		t.Fatal("expected syntax failure")
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatal("partial failed decode mutated filter")
	}
}
