package extension

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ResolutionState is the resolution result for one observed extension in a
// selected compile context.
type ResolutionState string

const (
	StateSupported           ResolutionState = "supported"
	StateOpaquePreserved     ResolutionState = "opaque-preserved"
	StateUnsupportedCritical ResolutionState = "unsupported-critical"
)

// UnsupportedReason is stable machine-readable evidence for fail-closed
// semantic-critical extension resolution.
type UnsupportedReason string

const (
	ReasonCapabilityMissing   UnsupportedReason = "capability_missing"
	ReasonVersionIncompatible UnsupportedReason = "version_incompatible"
)

// Identity names an extension independently of registration order or raw
// source ordering. Version is intentionally not part of Identity because it is
// matched explicitly by the capability registration.
type Identity struct {
	Namespace string
	Kind      string
	Scope     string
}

func (id Identity) String() string {
	return strings.Join([]string{id.Namespace, id.Kind, id.Scope}, "/")
}

func (id Identity) valid() bool {
	return id.Namespace != "" && id.Kind != "" && id.Scope != ""
}

// Requirement is derived from an explicit extension contract or adapter. A
// non-critical requirement is preserved without interpretation.
type Requirement struct {
	Identity   Identity
	Version    string
	Capability string
	Critical   bool
}

// RendererContext identifies the dialect evidence supplied by the already
// selected Renderer. It never selects or looks up a Renderer.
type RendererContext struct {
	Dialect string
}

// Registration declares one exact extension capability version supported by a
// selected Renderer dialect. Compatible ranges can be added without changing
// Requirement or Resolution semantics.
type Registration struct {
	Identity   Identity
	Version    string
	Capability string
	Renderer   RendererContext
}

// Resolution is deterministic, bounded support evidence. It never contains raw
// extension payloads.
type Resolution struct {
	State      ResolutionState
	Reason     UnsupportedReason
	Identity   Identity
	Version    string
	Capability string
	Renderer   RendererContext
}

var ErrDuplicateRegistration = errors.New("duplicate extension capability registration")

// Registry stores semantic-critical extension capability registrations.
type Registry struct {
	registrations map[string]Registration
}

func NewRegistry() *Registry {
	return &Registry{registrations: make(map[string]Registration)}
}

func (r *Registry) Register(reg Registration) error {
	if r == nil {
		return errors.New("extension capability registry is nil")
	}
	if !reg.Identity.valid() || reg.Version == "" || reg.Capability == "" || reg.Renderer.Dialect == "" {
		return fmt.Errorf("invalid extension capability registration: %#v", reg)
	}
	key := registrationKey(reg.Identity, reg.Capability, reg.Renderer)
	if existing, ok := r.registrations[key]; ok {
		return fmt.Errorf("%w: %s capability=%s dialect=%s existing_version=%s new_version=%s", ErrDuplicateRegistration, reg.Identity.String(), reg.Capability, reg.Renderer.Dialect, existing.Version, reg.Version)
	}
	r.registrations[key] = reg
	return nil
}

func (r *Registry) Resolve(req Requirement, renderer RendererContext) Resolution {
	result := Resolution{
		Identity:   req.Identity,
		Version:    req.Version,
		Capability: req.Capability,
		Renderer:   renderer,
	}
	if !req.Critical {
		result.State = StateOpaquePreserved
		return result
	}
	if r == nil {
		result.State = StateUnsupportedCritical
		result.Reason = ReasonCapabilityMissing
		return result
	}
	reg, ok := r.registrations[registrationKey(req.Identity, req.Capability, renderer)]
	if !ok {
		result.State = StateUnsupportedCritical
		result.Reason = ReasonCapabilityMissing
		return result
	}
	if req.Version == "" || reg.Version != req.Version {
		result.State = StateUnsupportedCritical
		result.Reason = ReasonVersionIncompatible
		return result
	}
	result.State = StateSupported
	return result
}

func (r *Registry) Registrations() []Registration {
	if r == nil {
		return nil
	}
	values := make([]Registration, 0, len(r.registrations))
	for _, reg := range r.registrations {
		values = append(values, reg)
	}
	sort.Slice(values, func(i, j int) bool {
		return registrationKey(values[i].Identity, values[i].Capability, values[i].Renderer) < registrationKey(values[j].Identity, values[j].Capability, values[j].Renderer)
	})
	return values
}

func registrationKey(identity Identity, capability string, renderer RendererContext) string {
	return strings.Join([]string{identity.Namespace, identity.Kind, identity.Scope, capability, renderer.Dialect}, "\x00")
}
