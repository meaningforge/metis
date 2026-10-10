package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPredicateRoundTripPreservesBooleanStructure(t *testing.T) {
	doc := []byte(`{"kind":"and","children":[{"kind":"filter","filter":{"field":"status","operator":"eq","value":"paid"}},{"kind":"or","children":[{"kind":"filter","filter":{"field":"region","operator":"eq","value":"APAC"}},{"kind":"not","child":{"kind":"filter","filter":{"field":"tier","operator":"is_null"}}}]}]}`)
	var predicate Predicate
	if err := json.Unmarshal(doc, &predicate); err != nil {
		t.Fatal(err)
	}
	if len(predicate) != 2 || predicate[1].Kind != PredicateOr || predicate[1].Children[1].Kind != PredicateNot {
		t.Fatalf("unexpected predicate: %#v", predicate)
	}
	encoded, err := json.Marshal(predicate)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Predicate
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if got := roundTrip.Leaves(); len(got) != 3 || got[0].Field != "status" || got[1].Field != "region" || got[2].Field != "tier" {
		t.Fatalf("leaf order changed: %#v", got)
	}
}

func TestPredicateRoundTripPreservesRelationshipExistence(t *testing.T) {
	doc := []byte(`{"kind":"exists","relationship":"orders_to_items","where":{"kind":"or","children":[{"kind":"filter","filter":{"field":"item_category","operator":"eq","value":"target"}},{"kind":"filter","filter":{"field":"item_tier","operator":"eq","value":"priority"}}]}}`)
	var predicate Predicate
	if err := json.Unmarshal(doc, &predicate); err != nil {
		t.Fatal(err)
	}
	if len(predicate) != 1 || predicate[0].Kind != PredicateExists || predicate[0].Relationship != "orders_to_items" || len(predicate[0].Children) != 1 {
		t.Fatalf("unexpected existence predicate: %#v", predicate)
	}
	encoded, err := json.Marshal(predicate)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Predicate
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if got := roundTrip.Leaves(); len(got) != 2 || got[0].Field != "item_category" || got[1].Field != "item_tier" {
		t.Fatalf("existence leaf order changed: %#v", got)
	}
}

func TestPredicateRootAndHasTheSameConjunctsForJSONAndGoConstruction(t *testing.T) {
	root := Logical(PredicateAnd,
		Logical(PredicateOr, Leaf("region", FilterEQ, "APAC"), Leaf("tier", FilterEQ, "enterprise")),
		Leaf("revenue", FilterGT, "100"),
	)
	programmatic := Predicate{root}
	encoded, err := json.Marshal(programmatic)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Predicate
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, want := len(programmatic.RootConjuncts()), len(decoded.RootConjuncts()); got != want || got != 2 {
		t.Fatalf("root conjuncts differ: programmatic=%d decoded=%d", got, want)
	}
}

func TestPredicateRejectsLegacyAndAmbiguousShapes(t *testing.T) {
	for _, doc := range []string{
		`[{"field":"region","operator":"eq","value":"APAC"}]`,
		`null`,
		`{"kind":"filter","filter":{"field":"region","operator":"eq","value":"APAC","extra":true}}`,
		`{"kind":"filter","filter":{"field":"region","field":"country","operator":"eq","value":"APAC"}}`,
		`{"kind":"filter","filter":{"field":"region","operator":"eq","value":"APAC"},"extra":true}`,
		`{"kind":"not","children":[]}`,
		`{"kind":"or","children":[{"kind":"filter","filter":{"field":"region","operator":"eq","value":"APAC"}}]}`,
		`{"kind":"exists","where":{"kind":"filter","filter":{"field":"kind","operator":"eq","value":"target"}}}`,
		`{"kind":"exists","relationship":"orders_to_items"}`,
		`{"kind":"exists","relationship":"orders_to_items","where":{"kind":"filter","filter":{"field":"kind","operator":"eq","value":"target"}},"child":{"kind":"filter","filter":{"field":"kind","operator":"eq","value":"target"}}}`,
	} {
		var predicate Predicate
		if err := json.Unmarshal([]byte(doc), &predicate); err == nil {
			t.Fatalf("accepted invalid predicate: %s", doc)
		}
	}
}

func TestPredicateRejectsProgrammaticCyclesAndBudgets(t *testing.T) {
	cycle := Logical(PredicateNot, Filter{})
	cycle.Children[0] = cycle
	if err := (Predicate{cycle}).Validate(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle validation = %v", err)
	}
	tooMany := make([]Filter, 33)
	for i := range tooMany {
		tooMany[i] = Leaf("region", FilterEQ, "APAC")
	}
	if err := (Predicate{Logical(PredicateOr, tooMany...)}).Validate(); err == nil {
		t.Fatal("accepted excessive fanout")
	}
	for _, value := range []any{map[string]any{"raw": "sql"}, []any{[]string{"nested"}}} {
		if err := (Predicate{Leaf("region", FilterEQ, value)}).Validate(); err == nil {
			t.Fatalf("accepted non-scalar programmatic operand: %#v", value)
		}
	}
}

func TestPredicateNestedNumericStringRemainsExact(t *testing.T) {
	const operand = "9007199254740993"
	var predicate Predicate
	err := json.Unmarshal([]byte(`{"kind":"not","child":{"kind":"filter","filter":{"field":"id","operator":"eq","value":"`+operand+`"}}}`), &predicate)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := predicate.Leaves()[0].Value.(string)
	if !ok || got != operand {
		t.Fatalf("numeric operand = %#v", predicate.Leaves()[0].Value)
	}
}

func TestPredicateRejectsNonStringOperand(t *testing.T) {
	var predicate Predicate
	err := json.Unmarshal([]byte(`{"kind":"filter","filter":{"field":"id","operator":"eq","value":1}}`), &predicate)
	if err == nil || !strings.Contains(err.Error(), "string") {
		t.Fatalf("validation = %v, want string operand error", err)
	}
}

func TestPredicateCountsTheWholeStringStructure(t *testing.T) {
	leaf := func(index int) string {
		return `{"kind":"filter","filter":{"field":"id","operator":"eq","value":"1"}}`
	}
	groups := make([]string, 3)
	next := 0
	for group := range groups {
		children := make([]string, 22)
		for i := range children {
			children[i] = leaf(next)
			next++
		}
		groups[group] = `{"kind":"and","children":[` + strings.Join(children, ",") + `]}`
	}
	doc := `{"kind":"and","children":[` + strings.Join(groups, ",") + `]}`
	var predicate Predicate
	err := json.Unmarshal([]byte(doc), &predicate)
	if err == nil || !strings.Contains(err.Error(), "maximum leaves") {
		t.Fatalf("validation order = %v, want structural leaf limit", err)
	}
}
