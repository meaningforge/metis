package ossie

import "testing"

func TestSemanticModelDataSourceIsStrictAndPreservesOtherExtensions(t *testing.T) {
	model := &SemanticModel{CustomExtensions: []CustomExtension{
		{VendorName: "EXAMPLE", Data: `{"anything":true}`},
		{VendorName: MetisExtensionVendor, Data: `{"kind":"data_source","name":"warehouse"}`},
	}}
	placement, ok, err := SemanticModelDataSource(model)
	if err != nil || !ok || placement.Name != "warehouse" {
		t.Fatalf("placement = %#v, %t, %v", placement, ok, err)
	}
	if model.CustomExtensions[0].VendorName != "EXAMPLE" {
		t.Fatal("unrelated extension changed")
	}

	for _, data := range []string{
		`{"kind":"data_source","name":""}`,
		`{"kind":"data_source","name":" warehouse"}`,
		`{"kind":"data_source","name":"warehouse","extra":true}`,
	} {
		model := &SemanticModel{CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: data}}}
		if _, _, err := SemanticModelDataSource(model); err == nil {
			t.Fatalf("invalid placement accepted: %s", data)
		}
	}

	duplicate := &SemanticModel{CustomExtensions: []CustomExtension{
		{VendorName: MetisExtensionVendor, Data: `{"kind":"data_source","name":"warehouse"}`},
		{VendorName: MetisExtensionVendor, Data: `{"kind":"data_source","name":"crm"}`},
	}}
	if _, _, err := SemanticModelDataSource(duplicate); err == nil {
		t.Fatal("duplicate placement accepted")
	}
}
