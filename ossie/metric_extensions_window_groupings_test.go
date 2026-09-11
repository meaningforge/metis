package ossie

import "testing"

func TestSemiAdditiveWindowGroupingsContract(t *testing.T) {
	cases := []struct {
		name       string
		data       string
		want       []string
		wantRollup string
		wantErr    bool
	}{
		{name: "omitted remains valid", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last"}`, wantRollup: SemiAdditiveRollupSum},
		{name: "single grouping keeps legacy sum", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"]}`, want: []string{"warehouse"}, wantRollup: SemiAdditiveRollupSum},
		{name: "explicit min rollup", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"],"rollup_aggregation":"min"}`, want: []string{"warehouse"}, wantRollup: SemiAdditiveRollupMin},
		{name: "explicit max rollup composes with tie break and null skip", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"first","tie_break_dimension":"snapshot_sequence","null_policy":"skip","window_groupings":["warehouse","region"],"rollup_aggregation":"max"}`, want: []string{"warehouse", "region"}, wantRollup: SemiAdditiveRollupMax},
		{name: "duplicate grouping rejected", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse","warehouse"]}`, wantErr: true},
		{name: "time key cannot be grouping", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["snapshot_date"]}`, wantErr: true},
		{name: "empty grouping rejected", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":[""]}`, wantErr: true},
		{name: "rollup without grouping rejected", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","rollup_aggregation":"max"}`, wantErr: true},
		{name: "non composable average rejected", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"],"rollup_aggregation":"average"}`, wantErr: true},
		{name: "count distinct rejected", data: `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"],"rollup_aggregation":"count_distinct"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metric := &Metric{CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: tc.data}}}
			spec, ok, err := SemiAdditiveSpec(metric)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected window-grouping validation error; spec=%#v ok=%v", spec, ok)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("semi-additive extension was not detected")
			}
			if len(spec.WindowGroupings) != len(tc.want) {
				t.Fatalf("window groupings = %#v, want %#v", spec.WindowGroupings, tc.want)
			}
			for i := range tc.want {
				if spec.WindowGroupings[i] != tc.want[i] {
					t.Fatalf("window groupings = %#v, want %#v", spec.WindowGroupings, tc.want)
				}
			}
			if got := EffectiveSemiAdditiveRollupAggregation(spec); got != tc.wantRollup {
				t.Fatalf("rollup = %q, want %q", got, tc.wantRollup)
			}
		})
	}
}
