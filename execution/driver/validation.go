package driver

import (
	"context"

	"github.com/meaningforge/metis/compiler/artifact"
)

// ValidationEvidence records engine planning acceptance, never result correctness.
// Native error text and EXPLAIN output must remain inside the backend.
type ValidationEvidence struct {
	Outcome string `json:"outcome"`
	Method  string `json:"method"`
}

// CompiledQueryValidator is optional and must not execute the compiled SELECT.
// Unsupported parameter shapes return unsupported without sending a query.
type CompiledQueryValidator interface {
	ValidateCompiled(context.Context, *artifact.CompiledQuery, CatalogLimits) (ValidationEvidence, error)
}
