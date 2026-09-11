package manifest

import (
	"github.com/meaningforge/metis/ossie"
	"reflect"
	"testing"
)

func ontologyFixture() (*ossie.Document, map[string]*ModelIndex) {
	doc := &ossie.Document{Ontology: []map[string]any{
		{"concept": "Money", "type": "ValueType", "extends": []string{"Decimal"}},
		{"concept": "Revenue", "type": "ValueType", "extends": []string{"Money"}},
	}, OntologyMappings: []map[string]any{{"name": "mapping", "concept_mappings": []map[string]any{
		{"concept": "Revenue", "object_mappings": []map[string]any{{"expression": "orders.amount"}}},
	}}}}
	models := map[string]*ModelIndex{"sales": {Fields: map[string]*FieldHandle{
		"orders.amount": {Dataset: "orders", Field: &ossie.Field{Name: "amount", Datatype: ossie.DataTypeDecimal, Dimension: &ossie.Dimension{}}},
	}}}
	return doc, models
}

func TestOntologyBindingGraphAndOwnership(t *testing.T) {
	doc, models := ontologyFixture()
	index := buildOntologyResolution(doc, models)
	if !index.Ready() {
		t.Fatal(index.Diagnostics())
	}
	targets, ok := index.Targets("money")
	if !ok || len(targets) != 1 || targets[0].Concept != "Revenue" {
		t.Fatalf("targets=%v ok=%v", targets, ok)
	}
	targets[0].Field = "changed"
	again, _ := index.Targets("Money")
	if again[0].Field != "amount" {
		t.Fatal("mutable target")
	}
	if _, ok := index.Targets("missing"); ok {
		t.Fatal("unknown concept")
	}
	doc.Ontology[0], doc.Ontology[1] = doc.Ontology[1], doc.Ontology[0]
	reordered := buildOntologyResolution(doc, models)
	other, _ := reordered.Targets("Money")
	if !reflect.DeepEqual(again, other) {
		t.Fatal("order changed binding")
	}
	// Supertype mappings do not become subtype evidence.
	doc.OntologyMappings[0]["concept_mappings"].([]map[string]any)[0]["concept"] = "Money"
	index = buildOntologyResolution(doc, models)
	targets, ok = index.Targets("Revenue")
	if !ok || len(targets) != 0 {
		t.Fatal("supertype leaked downward")
	}
}

func TestOntologyInvalidAndUnsupportedMappings(t *testing.T) {
	cases := []struct {
		name, code string
		mutate     func(*ossie.Document, map[string]*ModelIndex)
		ready      bool
	}{
		{"cycle", "ONTOLOGY_GRAPH_INVALID", func(d *ossie.Document, _ map[string]*ModelIndex) { d.Ontology[0]["extends"] = []string{"Revenue"} }, false},
		{"unknown parent", "ONTOLOGY_GRAPH_INVALID", func(d *ossie.Document, _ map[string]*ModelIndex) { d.Ontology[0]["extends"] = []string{"External"} }, false},
		{"collision", "ONTOLOGY_GRAPH_INVALID", func(d *ossie.Document, _ map[string]*ModelIndex) {
			d.Ontology = append(d.Ontology, map[string]any{"concept": "money", "type": "ValueType"})
		}, false},
		{"builtin shadow", "ONTOLOGY_GRAPH_INVALID", func(d *ossie.Document, _ map[string]*ModelIndex) { d.Ontology[0]["concept"] = "Decimal" }, false},
		{"cross type", "ONTOLOGY_GRAPH_INVALID", func(d *ossie.Document, _ map[string]*ModelIndex) { d.Ontology[0]["extends"] = []string{"Any"} }, false},
		{"multiple roots", "ONTOLOGY_GRAPH_INVALID", func(d *ossie.Document, _ map[string]*ModelIndex) {
			d.Ontology[0]["extends"] = []string{"Decimal", "String"}
		}, false},
		{"type mismatch", "ONTOLOGY_MAPPING_TYPE_MISMATCH", func(_ *ossie.Document, m map[string]*ModelIndex) {
			m["sales"].Fields["orders.amount"].Field.Datatype = ossie.DataTypeString
		}, false},
		{"missing type", "ONTOLOGY_MAPPING_TYPE_UNPROVEN", func(_ *ossie.Document, m map[string]*ModelIndex) {
			m["sales"].Fields["orders.amount"].Field.Datatype = ""
		}, false},
		{"not queryable", "ONTOLOGY_MAPPING_TARGET_NOT_QUERYABLE", func(_ *ossie.Document, m map[string]*ModelIndex) {
			m["sales"].Fields["orders.amount"].Field.Dimension = nil
		}, true},
		{"unbound", "ONTOLOGY_MAPPING_TARGET_INVALID", func(_ *ossie.Document, m map[string]*ModelIndex) { delete(m["sales"].Fields, "orders.amount") }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, m := ontologyFixture()
			tc.mutate(d, m)
			i := buildOntologyResolution(d, m)
			if i.Ready() != tc.ready {
				t.Fatalf("ready=%v diagnostics=%v", i.Ready(), i.Diagnostics())
			}
			found := false
			for _, diag := range i.Diagnostics() {
				if diag.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s: %v", tc.code, i.Diagnostics())
			}
		})
	}
}

func TestOntologySimpleEntityIdentifier(t *testing.T) {
	for _, multiplicity := range []string{"OneToOne", "ManyToOne", ""} {
		t.Run(multiplicity, func(t *testing.T) {
			d, m := ontologyFixture()
			d.Ontology = append(d.Ontology, map[string]any{"concept": "Account", "type": "EntityType", "identify_by": []string{"id"}, "relationships": []map[string]any{
				{"name": "id", "multiplicity": multiplicity, "roles": []map[string]any{{"concept": "Money"}}},
			}})
			d.OntologyMappings[0]["concept_mappings"].([]map[string]any)[0]["concept"] = "Account"
			i := buildOntologyResolution(d, m)
			if i.Ready() != (multiplicity == "OneToOne") {
				t.Fatal(i.Diagnostics())
			}
		})
	}
}

func TestOntologyCompoundMappingsDoNotCreateCandidates(t *testing.T) {
	for _, source := range []string{"orders.amount + 1", "CAST(orders.amount AS DECIMAL)", "1"} {
		d, m := ontologyFixture()
		object := d.OntologyMappings[0]["concept_mappings"].([]map[string]any)[0]["object_mappings"].([]map[string]any)[0]
		object["expression"] = source
		i := buildOntologyResolution(d, m)
		if !i.Ready() {
			t.Fatal(i.Diagnostics())
		}
		targets, _ := i.Targets("Revenue")
		if len(targets) != 0 {
			t.Fatal(targets)
		}
	}
}

func TestOntologyValueTypeMatrix(t *testing.T) {
	for _, root := range ontologyBuiltins[1:] {
		for _, datatype := range []string{"Boolean", "Date", "DateTime", "DateTimeTz", "Decimal", "Float", "Integer", "String", "Time", "Opaque", ""} {
			t.Run(root+"/"+datatype, func(t *testing.T) {
				d, m := ontologyFixture()
				d.Ontology[0]["extends"] = []string{root}
				m["sales"].Fields["orders.amount"].Field.Datatype = ossie.DataType(datatype)
				index := buildOntologyResolution(d, m)
				want := datatype == root || (root == "DateTime" && datatype == "DateTimeTz")
				if index.Ready() != want {
					t.Fatalf("ready=%v want=%v diagnostics=%v", index.Ready(), want, index.Diagnostics())
				}
			})
		}
	}
}

func TestOntologyDiamondDeduplicatesTraversal(t *testing.T) {
	d, m := ontologyFixture()
	d.Ontology = append(d.Ontology, map[string]any{"concept": "OtherMoney", "type": "ValueType", "extends": []string{"Decimal"}})
	d.Ontology[1]["extends"] = []string{"Money", "OtherMoney"}
	index := buildOntologyResolution(d, m)
	if !index.Ready() {
		t.Fatal(index.Diagnostics())
	}
	targets, _ := index.Targets("Decimal")
	if len(targets) != 1 {
		t.Fatal(targets)
	}
	d.Ontology[1]["extends"] = []string{"OtherMoney", "Money"}
	second := buildOntologyResolution(d, m)
	again, _ := second.Targets("Decimal")
	if !reflect.DeepEqual(targets, again) {
		t.Fatal("parent order changes candidates")
	}
}
