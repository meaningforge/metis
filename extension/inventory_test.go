package extension

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/ossie"
)

type testInterpreter struct {
	vendor ossie.Vendor
}

func (i testInterpreter) Vendor() ossie.Vendor { return i.vendor }

func (i testInterpreter) Requirements(location Location, _ ossie.CustomExtension) ([]Requirement, error) {
	return []Requirement{{
		Identity: Identity{Namespace: string(i.vendor), Kind: "critical", Scope: location.Scope},
		Version:  "1", Capability: "critical", Critical: true,
	}}, nil
}

func TestInventoryUsesOnlyExplicitInterpreters(t *testing.T) {
	inventory := NewInventory()
	if err := inventory.Register(testInterpreter{vendor: "known.example"}); err != nil {
		t.Fatal(err)
	}
	model := &ossie.SemanticModel{
		Name: "sales",
		CustomExtensions: []ossie.CustomExtension{
			{VendorName: "unknown.example", Data: "opaque"},
			{VendorName: "known.example", Data: "critical"},
		},
	}

	got, err := inventory.RequirementsForModel(model)
	if err != nil {
		t.Fatal(err)
	}
	want := []Requirement{{Identity: Identity{Namespace: "known.example", Kind: "critical", Scope: "semantic_model"}, Version: "1", Capability: "critical", Critical: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requirements = %#v, want %#v", got, want)
	}
}

func TestInventoryRejectsDuplicateVendorInterpreter(t *testing.T) {
	inventory := NewInventory()
	interpreter := testInterpreter{vendor: "known.example"}
	if err := inventory.Register(interpreter); err != nil {
		t.Fatal(err)
	}
	if err := inventory.Register(interpreter); !errors.Is(err, ErrDuplicateInterpreter) {
		t.Fatalf("duplicate interpreter error = %v", err)
	}
}
