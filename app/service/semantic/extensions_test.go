package semantic

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

type serviceTestExtensionInterpreter struct {
	vendor     ossie.Vendor
	version    string
	capability string
	critical   bool
}

func (i serviceTestExtensionInterpreter) Vendor() ossie.Vendor { return i.vendor }

func (i serviceTestExtensionInterpreter) Requirements(location extension.Location, _ ossie.CustomExtension) ([]extension.Requirement, error) {
	return []extension.Requirement{{
		Identity: extension.Identity{Namespace: string(i.vendor), Kind: "contract", Scope: location.Scope},
		Version:  i.version, Capability: i.capability, Critical: i.critical,
	}}, nil
}

func TestValidateExtensionCapabilitiesUsesSelectedTarget(t *testing.T) {
	const vendor ossie.Vendor = "critical.example"
	inventory := extension.NewInventory()
	if err := inventory.Register(serviceTestExtensionInterpreter{vendor: vendor, version: "1", capability: "calendar", critical: true}); err != nil {
		t.Fatal(err)
	}
	model := &ossie.SemanticModel{Name: "sales", CustomExtensions: []ossie.CustomExtension{{VendorName: vendor, Data: "preserved"}}}
	doris := mustRenderer(t, "DORIS")
	clickhouse := mustRenderer(t, "CLICKHOUSE")

	capabilities := extension.NewRegistry()
	if err := capabilities.Register(extension.Registration{
		Identity: extension.Identity{Namespace: string(vendor), Kind: "contract", Scope: "semantic_model"},
		Version:  "1", Capability: "calendar", Renderer: extension.RendererContext{Dialect: string(doris.SQLDialect())},
	}); err != nil {
		t.Fatal(err)
	}
	service := &CompileService{extensionInventory: inventory, extensionCapabilities: capabilities}
	if err := service.validateExtensionCapabilities(model, doris); err != nil {
		t.Fatalf("supported target validation failed: %v", err)
	}

	err := service.validateExtensionCapabilities(model, clickhouse)
	assertUnsupportedSemanticExtension(t, err, extension.ReasonCapabilityMissing, clickhouse)
}

func TestValidateExtensionCapabilitiesRejectsVersionMismatch(t *testing.T) {
	const vendor ossie.Vendor = "critical.example"
	inventory := extension.NewInventory()
	if err := inventory.Register(serviceTestExtensionInterpreter{vendor: vendor, version: "2", capability: "calendar", critical: true}); err != nil {
		t.Fatal(err)
	}
	target := mustRenderer(t, "DORIS")
	capabilities := extension.NewRegistry()
	if err := capabilities.Register(extension.Registration{
		Identity: extension.Identity{Namespace: string(vendor), Kind: "contract", Scope: "semantic_model"},
		Version:  "1", Capability: "calendar", Renderer: extension.RendererContext{Dialect: string(target.SQLDialect())},
	}); err != nil {
		t.Fatal(err)
	}
	service := &CompileService{extensionInventory: inventory, extensionCapabilities: capabilities}
	err := service.validateExtensionCapabilities(
		&ossie.SemanticModel{Name: "sales", CustomExtensions: []ossie.CustomExtension{{VendorName: vendor, Data: "preserved"}}},
		target,
	)
	assertUnsupportedSemanticExtension(t, err, extension.ReasonVersionIncompatible, target)
}

func TestValidateExtensionCapabilitiesLeavesUnknownVendorOpaque(t *testing.T) {
	service := &CompileService{extensionInventory: extension.NewInventory(), extensionCapabilities: extension.NewRegistry()}
	model := &ossie.SemanticModel{Name: "sales", CustomExtensions: []ossie.CustomExtension{{VendorName: "unknown.example", Data: "opaque"}}}
	if err := service.validateExtensionCapabilities(model, mustRenderer(t, "DORIS")); err != nil {
		t.Fatalf("unknown extension should remain opaque-preserved: %v", err)
	}
}

func assertUnsupportedSemanticExtension(t *testing.T, err error, reason extension.UnsupportedReason, renderer interface{ SQLDialect() sql.SQLDialect }) {
	t.Helper()

	var unsupported *extension.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want extension.UnsupportedError", err)
	}
	if unsupported.Resolution.Reason != reason {
		t.Fatalf("reason = %s, want %s", unsupported.Resolution.Reason, reason)
	}

	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) {
		t.Fatalf("error = %v, want serrors.Error", err)
	}
	if semanticErr.Code != serrors.ErrUnsupportedSemanticExtension {
		t.Fatalf("code = %s, want %s", semanticErr.Code, serrors.ErrUnsupportedSemanticExtension)
	}
	wantDetails := map[string]string{
		"extension_identity": unsupported.Resolution.Identity.String(),
		"capability":         unsupported.Resolution.Capability,
		"version":            unsupported.Resolution.Version,
		"dialect":            string(renderer.SQLDialect()),
		"reason":             string(reason),
	}
	if len(semanticErr.Details) != len(wantDetails) {
		t.Fatalf("details = %#v, want bounded fields %#v", semanticErr.Details, wantDetails)
	}
	for key, want := range wantDetails {
		if got, _ := semanticErr.Details[key].(string); got != want {
			t.Fatalf("details[%q] = %q, want %q", key, got, want)
		}
	}
	if _, leaked := semanticErr.Details["data"]; leaked {
		t.Fatalf("raw extension payload leaked into details: %#v", semanticErr.Details)
	}
}
