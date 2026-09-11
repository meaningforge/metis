package main

import (
	"github.com/meaningforge/metis/execution/backend"
)

// defaultBackends declares the executable Backends linked into the metis
// binary. Compile-only Renderer registration is assembled independently.
func defaultBackends() (*backend.BackendRegistry, error) {
	return backend.NewBackendRegistry(defaultBackendBindings()...)
}
