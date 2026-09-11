package query

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSemanticQueryJSONRoundTrip(t *testing.T) {
	doc := []byte(`{
		"project":"demo",
		"model":"orders",
		"metrics":[{"name":"revenue"}],
		"dimensions":[{"name":"order_date","grain":"month"}],
		"filters":[
			{"field":"country","operator":"eq","value":"US"},
			{"field":"revenue","operator":"between","value":[10,20]}
		],
		"order_by":[{"field":"revenue","direction":"desc"}],
		"limit":100
	}`)

	var first SemanticQuery
	if err := json.Unmarshal(doc, &first); err != nil {
		t.Fatalf("unmarshal semantic query: %v", err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal semantic query: %v", err)
	}
	var second SemanticQuery
	if err := json.Unmarshal(encoded, &second); err != nil {
		t.Fatalf("round-trip unmarshal semantic query: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("semantic query changed across JSON round-trip:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestSemanticQueryRejectsEmbeddedTarget(t *testing.T) {
	var q SemanticQuery
	err := json.Unmarshal([]byte(`{"project":"demo","model":"orders","metrics":[{"name":"revenue"}],"target":{"engine":"native","dialect":"doris"}}`), &q)
	if err == nil {
		t.Fatal("expected query-level target to be rejected")
	}
}

func TestSemanticQueryRejectsEmbeddedExecutionBinding(t *testing.T) {
	var q SemanticQuery
	err := json.Unmarshal([]byte(`{"project":"demo","model":"orders","metrics":[{"name":"revenue"}],"execution_binding":"doris-prod"}`), &q)
	if err == nil {
		t.Fatal("expected query-level execution binding to be rejected")
	}
}

func TestFilterJSONRejectsObjectOperand(t *testing.T) {
	var filter Filter
	err := json.Unmarshal([]byte(`{"field":"country","operator":"eq","value":{"name":"US"}}`), &filter)
	if err == nil {
		t.Fatal("expected object filter operand to be rejected")
	}
}

func TestFilterJSONRejectsNestedArrayOperand(t *testing.T) {
	var filter Filter
	err := json.Unmarshal([]byte(`{"field":"country","operator":"in","value":[["US"],"JP"]}`), &filter)
	if err == nil {
		t.Fatal("expected nested filter array operand to be rejected")
	}
}

func TestFilterJSONAcceptsNaturalScalarAndFlatArrayOperands(t *testing.T) {
	cases := []string{
		`{"field":"country","operator":"eq","value":"US"}`,
		`{"field":"active","operator":"eq","value":true}`,
		`{"field":"revenue","operator":"gte","value":10.5}`,
		`{"field":"country","operator":"in","value":["US","JP",null]}`,
		`{"field":"deleted_at","operator":"is_null"}`,
	}
	for _, tc := range cases {
		var filter Filter
		if err := json.Unmarshal([]byte(tc), &filter); err != nil {
			t.Fatalf("unmarshal %s: %v", tc, err)
		}
	}
}
