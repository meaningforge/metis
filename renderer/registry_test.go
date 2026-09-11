package renderer_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/clickhouse"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/renderer/duckdb"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestBuiltInRegistryContainsSupportedDialects(t *testing.T) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	names := registry.Dialects()
	want := []sql.SQLDialect{clickhouse.Dialect, doris.Dialect, duckdb.Dialect}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("dialects = %#v, want %#v", names, want)
	}

	for _, name := range want {
		selected, err := registry.Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if selected.SQLDialect() != name {
			t.Fatalf("dialect name = %q, want %q", selected.SQLDialect(), name)
		}
	}
}

func TestRegistryResolveIsCaseAndWhitespaceInsensitive(t *testing.T) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Resolve("  CLICKHOUSE  ")
	if err != nil {
		t.Fatal(err)
	}
	if selected.SQLDialect() != clickhouse.Dialect {
		t.Fatalf("dialect name = %q", selected.SQLDialect())
	}
}

func TestRegistryRejectsUnknownAndRemovedANSIIdentities(t *testing.T) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []sql.SQLDialect{"snowflake", "ansi"} {
		if _, err := registry.Resolve(dialect); err == nil {
			t.Fatalf("Resolve(%q) succeeded, want error", dialect)
		}
	}
}

func TestRegistryRejectsDuplicateRegistration(t *testing.T) {
	registry, err := renderer.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(duckdb.Renderer{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(duckdb.Renderer{}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate registration error = %v", err)
	}
}

func TestRegistryRejectsMissingRenderer(t *testing.T) {
	registry, err := renderer.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(nil); err == nil {
		t.Fatal("Register(nil) succeeded, want fail-closed error")
	}
	var typedNil *duckdb.Renderer
	if err := registry.Register(typedNil); err == nil {
		t.Fatal("Register(typed nil) succeeded, want fail-closed error")
	}
}

func TestRegistryConstructorRejectsDuplicateNormalizedName(t *testing.T) {
	if _, err := renderer.NewRegistry(duckdb.Renderer{}, duckdb.Renderer{}); err == nil {
		t.Fatal("NewRegistry accepted duplicate dialect names")
	}
}

func TestRegistryFreezeRejectsMutation(t *testing.T) {
	registry, err := renderer.NewRegistry(duckdb.Renderer{})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	if err := registry.Register(doris.Renderer{}); err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("Register after Freeze error = %v", err)
	}
}
