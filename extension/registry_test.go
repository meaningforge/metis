package extension

import (
	"errors"
	"reflect"
	"testing"
)

func TestRegistryResolutionStates(t *testing.T) {
	identity := Identity{Namespace: "example", Kind: "metric_window", Scope: "metric"}
	target := RendererContext{Dialect: "DORIS"}
	registry := NewRegistry()
	if err := registry.Register(Registration{Identity: identity, Version: "2", Capability: "metric_window", Renderer: target}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		req    Requirement
		want   ResolutionState
		reason UnsupportedReason
	}{
		{name: "opaque non critical", req: Requirement{Identity: identity, Version: "99", Capability: "metric_window"}, want: StateOpaquePreserved},
		{name: "supported", req: Requirement{Identity: identity, Version: "2", Capability: "metric_window", Critical: true}, want: StateSupported},
		{name: "missing capability", req: Requirement{Identity: identity, Version: "2", Capability: "other", Critical: true}, want: StateUnsupportedCritical, reason: ReasonCapabilityMissing},
		{name: "version mismatch", req: Requirement{Identity: identity, Version: "3", Capability: "metric_window", Critical: true}, want: StateUnsupportedCritical, reason: ReasonVersionIncompatible},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := registry.Resolve(tc.req, target)
			if got.State != tc.want || got.Reason != tc.reason {
				t.Fatalf("resolution = %#v, want state=%s reason=%s", got, tc.want, tc.reason)
			}
		})
	}
}

func TestRegistryDoesNotLeakAcrossTargets(t *testing.T) {
	identity := Identity{Namespace: "example", Kind: "calendar", Scope: "project"}
	registry := NewRegistry()
	if err := registry.Register(Registration{Identity: identity, Version: "1", Capability: "fiscal_calendar", Renderer: RendererContext{Dialect: "DORIS"}}); err != nil {
		t.Fatal(err)
	}

	got := registry.Resolve(
		Requirement{Identity: identity, Version: "1", Capability: "fiscal_calendar", Critical: true},
		RendererContext{Dialect: "CLICKHOUSE"},
	)
	if got.State != StateUnsupportedCritical || got.Reason != ReasonCapabilityMissing {
		t.Fatalf("cross-target resolution = %#v", got)
	}
}

func TestRegistryRejectsDuplicateRegistration(t *testing.T) {
	registry := NewRegistry()
	reg := Registration{
		Identity:   Identity{Namespace: "example", Kind: "metric_window", Scope: "metric"},
		Version:    "1",
		Capability: "metric_window",
		Renderer:   RendererContext{Dialect: "DUCKDB"},
	}
	if err := registry.Register(reg); err != nil {
		t.Fatal(err)
	}
	reg.Version = "2"
	if err := registry.Register(reg); !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("duplicate registration error = %v", err)
	}
}

func TestRegistrationsAreDeterministic(t *testing.T) {
	registry := NewRegistry()
	second := Registration{Identity: Identity{Namespace: "z", Kind: "b", Scope: "metric"}, Version: "1", Capability: "b", Renderer: RendererContext{Dialect: "DORIS"}}
	first := Registration{Identity: Identity{Namespace: "a", Kind: "a", Scope: "metric"}, Version: "1", Capability: "a", Renderer: RendererContext{Dialect: "DUCKDB"}}
	if err := registry.Register(second); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if got, want := registry.Registrations(), []Registration{first, second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registrations = %#v, want %#v", got, want)
	}
}
