// Package backend defines the sole type-level binding from a DataSource type
// to one exact Renderer and Driver Factory.
package backend

import (
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
)

// Backend is one executable database-family implementation. It has no
// instance endpoint, credential, secret, or policy state.
type Backend struct {
	Type          datasource.Type
	Renderer      renderer.Renderer
	DriverFactory driver.Factory
}

// SQLDialect derives physical SQL identity from the exact Renderer bound to
// this Backend. Backend never stores a second dialect authority.
func (b Backend) SQLDialect() sql.SQLDialect {
	if isNil(b.Renderer) {
		return ""
	}
	return b.Renderer.SQLDialect()
}

// BackendRegistry maps each DataSource type to exactly one coherent Backend.
// Compile-only Renderer resolution remains independent in renderer.Registry.
type BackendRegistry struct {
	mu       sync.RWMutex
	backends map[datasource.Type]Backend
	frozen   bool
}

// NewBackendRegistry creates a type-level backend registry.
func NewBackendRegistry(backends ...Backend) (*BackendRegistry, error) {
	registry := &BackendRegistry{backends: make(map[datasource.Type]Backend, len(backends))}
	for _, backend := range backends {
		if err := registry.Register(backend); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register adds one Backend and fails if its type, Renderer, or
// DriverFactory is missing, incompatible, or already registered.
func (r *BackendRegistry) Register(backend Backend) error {
	if r == nil {
		return fmt.Errorf("Backend registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("Backend registry is frozen")
	}

	backendType := datasource.NormalizeType(string(backend.Type))
	if backendType == "" {
		return fmt.Errorf("Backend DataSource type is required")
	}
	if isNil(backend.Renderer) {
		return fmt.Errorf("Backend Renderer is required for DataSource type %q", backendType)
	}
	if isNil(backend.DriverFactory) {
		return fmt.Errorf("Backend DriverFactory is required for DataSource type %q", backendType)
	}

	rendererDialect := renderer.NormalizeSQLDialect(string(backend.Renderer.SQLDialect()))
	if rendererDialect == "" {
		return fmt.Errorf("Backend Renderer dialect is required for DataSource type %q", backendType)
	}
	factoryType := datasource.NormalizeType(string(backend.DriverFactory.DataSourceType()))
	if factoryType != backendType {
		return fmt.Errorf("Backend DriverFactory type %q does not match Backend DataSource type %q", factoryType, backendType)
	}

	if r.backends == nil {
		r.backends = make(map[datasource.Type]Backend)
	}
	if _, exists := r.backends[backendType]; exists {
		return fmt.Errorf("Backend for DataSource type %q is already registered", backendType)
	}
	backend.Type = backendType
	r.backends[backendType] = backend
	return nil
}

// Resolve returns the one Backend registered for a DataSource type.
func (r *BackendRegistry) Resolve(dataSourceType datasource.Type) (Backend, error) {
	if r == nil {
		return Backend{}, fmt.Errorf("Backend registry is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	normalized := datasource.NormalizeType(string(dataSourceType))
	if normalized == "" {
		return Backend{}, fmt.Errorf("DataSource type is required")
	}
	backend, exists := r.backends[normalized]
	if !exists {
		return Backend{}, fmt.Errorf("Backend for DataSource type %q is not registered", dataSourceType)
	}
	return backend, nil
}

// Types returns registered DataSource types in deterministic order.
func (r *BackendRegistry) Types() []datasource.Type {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]datasource.Type, 0, len(r.backends))
	for backendType := range r.backends {
		types = append(types, backendType)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// Freeze makes the registry an immutable runtime read model. Runtime mutation
// is deliberately rejected rather than synchronized as a hot-reload feature.
func (r *BackendRegistry) Freeze() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

// ValidateDataSources proves every configured DataSource type has one
// compatible Backend and lets the exact Driver validate its owned config.
func (r *BackendRegistry) ValidateDataSources(sources *datasource.DataSourceRegistry) error {
	if sources == nil {
		return fmt.Errorf("DataSource registry is required to validate DataSources")
	}
	if r == nil {
		return fmt.Errorf("Backend registry is required to validate DataSources")
	}
	for _, name := range sources.Names() {
		source, err := sources.Resolve(name)
		if err != nil {
			return err
		}
		backend, err := r.Resolve(source.Type)
		if err != nil {
			return fmt.Errorf("DataSource %q references an unavailable Backend: %w", name, err)
		}
		if err := backend.DriverFactory.ValidateConfig(source.ConfigSnapshot()); err != nil {
			return fmt.Errorf("DataSource %q configuration is invalid: %w", name, err)
		}
	}
	return nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
