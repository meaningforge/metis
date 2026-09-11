package ossie

import "testing"

func TestSemiAdditiveNullPolicyContract(t *testing.T) {
	cases := []struct {
		name       string
		data       string
		wantPolicy string
		wantErr    bool
	}{
		{name: "omitted preserves existing behavior", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last"}`, wantPolicy: ""},
		{name: "skip is accepted", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"first","null_policy":"skip"}`, wantPolicy: "skip"},
		{name: "skip composes with deterministic tie break", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","tie_break_dimension":"snapshot_sequence","null_policy":"skip"}`, wantPolicy: "skip"},
		{name: "unknown policy is rejected", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","null_policy":"keep"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metric := &Metric{CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: tc.data}}}
			spec, ok, err := SemiAdditiveSpec(metric)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected null-policy validation error; spec=%#v ok=%v", spec, ok)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("semi-additive extension was not detected")
			}
			if spec.NullPolicy != tc.wantPolicy {
				t.Fatalf("null policy = %q, want %q", spec.NullPolicy, tc.wantPolicy)
			}
		})
	}
}
