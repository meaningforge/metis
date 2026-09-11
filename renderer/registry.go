package renderer

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

// Capabilities describes concrete SQL-rendering behavior supplied by a
// Renderer. The Renderer contract already guarantees SQL output, so it carries
// no tautological output-kind list.
type Capabilities struct{}

// Renderer is the sole physical SQL compilation extension point. One Renderer
// instance is selected per compilation and supplies all dialect evidence used
// by semantic resolution, capability validation, and final rendering.
type Renderer interface {
	SQLDialect() sql.SQLDialect
	ExpressionDialect() string
	Capabilities() Capabilities
	Render(plan *sqlplan.Plan) (sql.SQLQuery, error)
}

// Registry resolves physical SQL Renderers by SQLDialect. It is assembled
// explicitly by a composition root; it never imports concrete Renderers.
type Registry struct {
	mu        sync.RWMutex
	renderers map[sql.SQLDialect]Renderer
	frozen    bool
}

// NewRegistry constructs a Registry and rejects invalid or duplicate
// registrations instead of panicking. Callers may Freeze it after assembly.
func NewRegistry(renderers ...Renderer) (*Registry, error) {
	r := &Registry{renderers: make(map[sql.SQLDialect]Renderer, len(renderers))}
	for _, renderer := range renderers {
		if err := r.Register(renderer); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds one Renderer and rejects duplicate normalized SQL dialects.
func (r *Registry) Register(renderer Renderer) error {
	if isNilRenderer(renderer) {
		return fmt.Errorf("SQL Renderer is required")
	}
	if r == nil {
		return fmt.Errorf("SQL Renderer registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("SQL Renderer registry is frozen")
	}
	dialect := NormalizeSQLDialect(string(renderer.SQLDialect()))
	if dialect == "" {
		return fmt.Errorf("SQL Renderer dialect is required")
	}
	if _, exists := r.renderers[dialect]; exists {
		return fmt.Errorf("SQL Renderer for dialect %q is already registered", dialect)
	}
	r.renderers[dialect] = renderer
	return nil
}

// Resolve returns the exact Renderer selected for dialect.
func (r *Registry) Resolve(dialect sql.SQLDialect) (Renderer, error) {
	if r == nil {
		return nil, fmt.Errorf("SQL Renderer registry is nil")
	}
	normalized := NormalizeSQLDialect(string(dialect))
	if normalized == "" {
		return nil, fmt.Errorf("SQL dialect is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	renderer, ok := r.renderers[normalized]
	if !ok {
		return nil, fmt.Errorf("SQL Renderer for dialect %q is not registered", dialect)
	}
	return renderer, nil
}

// Dialects returns registered SQL dialects in deterministic order.
func (r *Registry) Dialects() []sql.SQLDialect {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	dialects := make([]sql.SQLDialect, 0, len(r.renderers))
	for dialect := range r.renderers {
		dialects = append(dialects, dialect)
	}
	sort.Slice(dialects, func(i, j int) bool { return dialects[i] < dialects[j] })
	return dialects
}

// Freeze makes the Registry immutable. It remains safe for concurrent reads.
func (r *Registry) Freeze() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// NormalizeSQLDialect canonicalizes a caller-supplied SQL dialect name.
func NormalizeSQLDialect(name string) sql.SQLDialect {
	return sql.SQLDialect(strings.ToUpper(strings.TrimSpace(name)))
}

func isNilRenderer(renderer Renderer) bool {
	if renderer == nil {
		return true
	}
	value := reflect.ValueOf(renderer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
