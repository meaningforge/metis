package ossie

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestAssetGovernanceDefaultsAndOpaqueExtensionPreservation(t *testing.T) {
	extensions := []CustomExtension{
		{VendorName: "example.vendor", Data: `{"anything":true}`},
		{VendorName: MetisExtensionVendor, Data: `{"kind":"asset_governance","owner":"team:finance","lifecycle":"deprecated","certification":"certified","deprecation_date":"2026-10-01","replacement":"metric:sales.net_revenue","policy_tags":["internal","finance"]}`},
	}
	governance, ok, err := ParseAssetGovernance(extensions)
	if err != nil || !ok {
		t.Fatalf("governance = %#v, present = %v, err = %v", governance, ok, err)
	}
	if governance.Owner != "team:finance" || governance.Lifecycle != AssetLifecycleDeprecated || governance.Certification != AssetCertificationCertified || !reflect.DeepEqual(governance.PolicyTags, []string{"finance", "internal"}) {
		t.Fatalf("governance = %#v", governance)
	}
	if extensions[0].VendorName != "example.vendor" || extensions[0].Data != `{"anything":true}` {
		t.Fatalf("opaque extension changed: %#v", extensions[0])
	}
	defaults, err := EffectiveAssetGovernance(nil)
	if err != nil || defaults.Lifecycle != AssetLifecycleActive || defaults.Certification != AssetCertificationUncertified {
		t.Fatalf("defaults = %#v, err = %v", defaults, err)
	}
}

func TestAssetGovernanceReplacementValidation(t *testing.T) {
	load := func(first, second string) error {
		body := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
    metrics:
      - name: old_revenue
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
        custom_extensions:
          - vendor_name: METIS
            data: '` + first + `'
      - name: net_revenue
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
        custom_extensions:
          - vendor_name: METIS
            data: '` + second + `'
`
		_, err := NewLoader().Load([]byte(body))
		return err
	}
	if err := load(`{"kind":"asset_governance","lifecycle":"deprecated","replacement":"metric:sales.net_revenue"}`, `{"kind":"asset_governance"}`); err != nil {
		t.Fatalf("valid replacement: %v", err)
	}
	for name, values := range map[string][2]string{
		"missing":       {`{"kind":"asset_governance","lifecycle":"deprecated","replacement":"metric:sales.missing"}`, `{"kind":"asset_governance"}`},
		"cross kind":    {`{"kind":"asset_governance","lifecycle":"deprecated","replacement":"dataset:sales.orders"}`, `{"kind":"asset_governance"}`},
		"self":          {`{"kind":"asset_governance","lifecycle":"deprecated","replacement":"metric:sales.old_revenue"}`, `{"kind":"asset_governance"}`},
		"cycle":         {`{"kind":"asset_governance","lifecycle":"deprecated","replacement":"metric:sales.net_revenue"}`, `{"kind":"asset_governance","lifecycle":"deprecated","replacement":"metric:sales.old_revenue"}`},
		"unknown field": {`{"kind":"asset_governance","lifecyle":"deprecated"}`, `{"kind":"asset_governance"}`},
	} {
		t.Run(name, func(t *testing.T) {
			err := load(values[0], values[1])
			var semanticErr *serrors.Error
			if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidModel {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
