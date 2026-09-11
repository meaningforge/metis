package s2sbench

import (
	"context"
	"fmt"

	"github.com/meaningforge/metis/renderer/sql"
)

// V0CollectionConfig is the deterministic client-level compatibility seam
// introduced before live v0 moved to real external Agent CLIs. It remains useful
// for in-process harness tests, but live collection does not use it: the live
// path constructs AgentRunner instances and calls Collect directly.
type V0CollectionConfig struct {
	Manifest RunManifest

	RawClient    RawAssetsClient
	RawAssets    RawAssetsProvider
	RawExecution Execution

	MetisClient    MetisClient
	MetisCompiler  SemanticCompiler
	MetisDialect   sql.SQLDialect
	MetisExecution Execution
}

// CollectV0 validates the frozen manifest before any injected deterministic
// client can run, then adapts the legacy client seams into the common Runner
// contract. It selects no provider, credentials, transport, or live Agent.
func CollectV0(ctx context.Context, cfg V0CollectionConfig) (Collection, error) {
	if err := cfg.Manifest.ValidateForCollection(); err != nil {
		return Collection{}, fmt.Errorf("validate v0 collection manifest: %w", err)
	}
	if cfg.RawClient == nil {
		return Collection{}, fmt.Errorf("raw-assets model client is required")
	}
	if cfg.RawAssets == nil {
		return Collection{}, fmt.Errorf("raw-assets provider is required")
	}
	if cfg.RawExecution == nil {
		return Collection{}, fmt.Errorf("raw-assets execution is required")
	}
	if cfg.MetisClient == nil {
		return Collection{}, fmt.Errorf("Metis model client is required")
	}
	if cfg.MetisCompiler == nil {
		return Collection{}, fmt.Errorf("Metis semantic compiler is required")
	}
	if cfg.MetisExecution == nil {
		return Collection{}, fmt.Errorf("Metis execution is required")
	}

	rawRunner := &RawAssetsRunner{
		Client: cfg.RawClient,
		Assets: cfg.RawAssets,
	}
	metisRunner := &MetisRunner{
		Client:   cfg.MetisClient,
		Compiler: cfg.MetisCompiler,
		Dialect:  cfg.MetisDialect,
	}

	return Collect(ctx, cfg.Manifest, rawRunner, cfg.RawExecution, metisRunner, cfg.MetisExecution)
}

// CollectAndBuildV0Report keeps report generation strictly downstream of raw
// collection. The returned Collection remains the source of truth; the report
// is a deterministic derived view and can always be regenerated.
func CollectAndBuildV0Report(ctx context.Context, cfg V0CollectionConfig) (Collection, V0Report, error) {
	collection, err := CollectV0(ctx, cfg)
	if err != nil {
		return Collection{}, V0Report{}, err
	}
	report, err := BuildV0Report(collection)
	if err != nil {
		return Collection{}, V0Report{}, fmt.Errorf("build v0 report: %w", err)
	}
	return collection, report, nil
}
