package runner

import (
	"context"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
)

// ValidateCompiled uses the already compiled production route and its resource
// lifecycle. It never falls back to Execute or changes SQL parameters.
func (r *Runner) ValidateCompiled(ctx context.Context, route ResolvedDataSource, compiled *artifact.CompiledQuery) (result driver.ValidationEvidence, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.sources == nil || r.backends == nil {
		return result, executionError(ExecutionConfig, "validation runtime is unavailable")
	}
	if ctx.Err() != nil {
		return result, contextExecutionError(ctx.Err())
	}
	owned, err := artifact.SnapshotCompiledQuery(compiled)
	if err != nil || owned == nil {
		return result, executionError(ExecutionInvalidInput, "compiled query is required")
	}
	if err := validateQueryDialect(owned.SqlRenderResult, route.Backend); err != nil {
		return result, err
	}
	limits, err := effectiveExecutionOptions(route.Source.Policy, ExecutionOptions{Timeout: 30 * time.Second, MaxRows: driver.MaxCatalogColumns, MaxBytes: driver.MaxCatalogBytes})
	if err != nil {
		return result, executionError(ExecutionConfig, "validation policy is invalid")
	}
	entry, err := r.entryFor(route)
	if err != nil {
		return result, err
	}
	if err := entry.admission.acquire(ctx); err != nil {
		return result, err
	}
	defer entry.admission.release()
	opCtx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	remove := entry.trackLease(cancel)
	defer remove()
	runtime, err := entry.readyRuntime(opCtx, r.secrets)
	if err != nil {
		return result, err
	}
	executor, err := runtime.Acquire(opCtx)
	if !isNilLike(executor) {
		defer func() {
			if closeErr := executor.Close(); closeErr != nil && err == nil {
				result = driver.ValidationEvidence{}
				err = executionError(ExecutionCleanup, "validation cleanup failed")
			}
		}()
	}
	if err != nil || isNilLike(executor) {
		return result, executionError(ExecutionOpen, "validation lease is unavailable")
	}
	validator, ok := executor.(driver.CompiledQueryValidator)
	if !ok || isNilLike(validator) {
		return driver.ValidationEvidence{Outcome: "unsupported"}, nil
	}
	result, err = validator.ValidateCompiled(opCtx, owned, driver.CatalogLimits{MaxColumns: int(limits.MaxRows), MaxBytes: int(limits.MaxBytes)})
	if opCtx.Err() != nil {
		return driver.ValidationEvidence{}, contextExecutionError(opCtx.Err())
	}
	if err != nil {
		return driver.ValidationEvidence{}, executionError(ExecutionDriver, "engine validation is unavailable")
	}
	if (result.Outcome != "accepted" && result.Outcome != "unsupported") || (result.Method != "" && result.Method != "explain") {
		return driver.ValidationEvidence{}, executionError(ExecutionResultContract, "invalid validation evidence")
	}
	return result, nil
}
