package backend_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/renderer/duckdb"
)

type testDriverFactory struct {
	dataSourceType datasource.Type
}

func (f *testDriverFactory) DataSourceType() datasource.Type {
	return f.dataSourceType
}

func (*testDriverFactory) ValidateConfig(map[string]string) error { return nil }

func (*testDriverFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return nil, nil
}

func TestBackendRegistryResolvesOneCoherentBackend(t *testing.T) {
	renderer := doris.New()
	factory := &testDriverFactory{dataSourceType: "doris"}
	registry, err := backend.NewBackendRegistry(backend.Backend{
		Type:          " DORIS ",
		Renderer:      renderer,
		DriverFactory: factory,
	})
	if err != nil {
		t.Fatalf("NewBackendRegistry() error = %v", err)
	}

	backend, err := registry.Resolve("doris")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if backend.Type != "doris" {
		t.Fatalf("Backend.Type = %q, want doris", backend.Type)
	}
	if backend.SQLDialect() != "DORIS" {
		t.Fatalf("Backend.SQLDialect() = %q, want DORIS", backend.SQLDialect())
	}
	if backend.Renderer != renderer {
		t.Fatal("Resolve() returned a different Renderer instance")
	}
	if backend.DriverFactory != factory {
		t.Fatal("Resolve() returned a different DriverFactory instance")
	}
}

func TestBackendRegistryRejectsInvalidRegistrations(t *testing.T) {
	renderer := doris.New()
	validFactory := &testDriverFactory{dataSourceType: "doris"}
	var nilFactory *testDriverFactory

	tests := []struct {
		name    string
		backend backend.Backend
		want    string
	}{
		{
			name:    "missing type",
			backend: backend.Backend{Renderer: renderer, DriverFactory: validFactory},
			want:    "DataSource type is required",
		},
		{
			name:    "missing renderer",
			backend: backend.Backend{Type: "doris", DriverFactory: validFactory},
			want:    "Renderer is required",
		},
		{
			name:    "missing factory",
			backend: backend.Backend{Type: "doris", Renderer: renderer},
			want:    "DriverFactory is required",
		},
		{
			name:    "typed nil factory",
			backend: backend.Backend{Type: "doris", Renderer: renderer, DriverFactory: nilFactory},
			want:    "DriverFactory is required",
		},
		{
			name:    "factory type mismatch",
			backend: backend.Backend{Type: "doris", Renderer: renderer, DriverFactory: &testDriverFactory{dataSourceType: "duckdb"}},
			want:    "does not match Backend DataSource type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := backend.NewBackendRegistry()
			if err != nil {
				t.Fatalf("NewBackendRegistry() error = %v", err)
			}
			err = registry.Register(test.backend)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Register() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBackendRegistryRejectsDuplicatesAndMissingBackends(t *testing.T) {
	registry, err := backend.NewBackendRegistry(backend.Backend{
		Type:          "doris",
		Renderer:      doris.New(),
		DriverFactory: &testDriverFactory{dataSourceType: "doris"},
	})
	if err != nil {
		t.Fatalf("NewBackendRegistry() error = %v", err)
	}
	if err := registry.Register(backend.Backend{
		Type:          " DORIS ",
		Renderer:      doris.New(),
		DriverFactory: &testDriverFactory{dataSourceType: "doris"},
	}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("Register() duplicate error = %v", err)
	}
	if _, err := registry.Resolve("clickhouse"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Resolve() missing error = %v", err)
	}
}

func TestBackendRegistryDoesNotGrantRuntimeAuthorityToCompileOnlyDialects(t *testing.T) {
	registry, err := backend.NewBackendRegistry()
	if err != nil {
		t.Fatalf("NewBackendRegistry() error = %v", err)
	}
	compileRegistry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileRegistry.Resolve("DUCKDB"); err != nil {
		t.Fatalf("compile-only DuckDB renderer resolution error = %v", err)
	}
	if _, err := registry.Resolve("duckdb"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("BackendRegistry.Resolve(duckdb) error = %v, want missing Backend", err)
	}
}

func TestBackendRegistryTypesAreSorted(t *testing.T) {
	duckDBRenderer := duckdb.New()
	dorisRenderer := doris.New()
	registry, err := backend.NewBackendRegistry(
		backend.Backend{Type: "doris", Renderer: dorisRenderer, DriverFactory: &testDriverFactory{dataSourceType: "doris"}},
		backend.Backend{Type: "duckdb", Renderer: duckDBRenderer, DriverFactory: &testDriverFactory{dataSourceType: "duckdb"}},
	)
	if err != nil {
		t.Fatalf("NewBackendRegistry() error = %v", err)
	}
	want := []datasource.Type{"doris", "duckdb"}
	got := registry.Types()
	if len(got) != len(want) {
		t.Fatalf("Types() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Types() = %v, want %v", got, want)
		}
	}
}
