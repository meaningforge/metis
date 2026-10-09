package query

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestFilterNumberPrecisionAtSharedBoundary(t *testing.T) {
	bad := []string{"9007199254740993", "-9007199254740993", "1000000000000000100", "0.10000000000000000001", "1e400", "1e-400", "1e99999", "1e-324"}
	for _, literal := range bad {
		for _, operand := range []string{literal, "[9007199254740992," + literal + "]", "[" + literal + ",1]"} {
			for _, outerUseNumber := range []bool{false, true} {
				var q SemanticQuery
				body := `{"filters":{"kind":"filter","filter":{"field":"amount","operator":"between","value":` + operand + `}}}`
				d := json.NewDecoder(bytes.NewBufferString(body))
				if outerUseNumber {
					d.UseNumber()
				}
				err := d.Decode(&q)
				if err == nil {
					t.Fatalf("accepted lossy operand %s", operand)
				}
				if strings.Contains(err.Error(), literal) {
					t.Fatalf("error disclosed operand: %v", err)
				}
			}
		}
	}
	for _, literal := range []string{"0.1", "1.25", "1e3", "9007199254740992", "9007199254740994", "-0", "0e-999", "[0.1,2]", `"9007199254740993"`, "true", "null"} {
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
}

func TestFilterPrecisionFailureDoesNotMutateReceiver(t *testing.T) {
	f := Filter{Field: "existing", Operator: FilterEQ, Value: "old"}
	want := f
	if err := json.Unmarshal([]byte(`{"field":"new","value":[1,9007199254740993]}`), &f); err == nil {
		t.Fatal("expected precision failure")
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatal("partial failed decode mutated filter")
	}
}
