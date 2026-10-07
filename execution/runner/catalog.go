package runner

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/meaningforge/metis/execution/driver"
)

// DescribeRelations shares execution admission, secret resolution, pool lifetime,
// and lease cancellation. Catalog work is not compiled query execution and has
// no arbitrary SQL surface. Results are complete or absent, never partial.
func (r *Runner) DescribeRelations(ctx context.Context, name string, refs []driver.CatalogReference) (result []driver.CatalogRelation, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if ctx.Err() != nil {
		return nil, contextExecutionError(ctx.Err())
	}
	if len(refs) == 0 || len(refs) > driver.MaxCatalogRelations {
		return nil, executionError(ExecutionInvalidInput, "catalog requires 1 through 200 relations")
	}
	resolved, err := r.ResolveDataSource(name)
	if err != nil {
		return nil, err
	}
	seenIDs, seenParts := map[string]bool{}, map[string]bool{}
	owned := make([]driver.CatalogReference, len(refs))
	for i, ref := range refs {
		if driver.ValidateCatalogReference(string(resolved.Source.Type), ref) != nil {
			return nil, executionError(ExecutionInvalidInput, "catalog requires exact qualified relation identifiers")
		}
		parts, _ := json.Marshal(ref.Parts)
		if seenIDs[ref.ID] || seenParts[string(parts)] {
			return nil, executionError(ExecutionInvalidInput, "catalog selectors must have unique IDs and physical identities")
		}
		seenIDs[ref.ID], seenParts[string(parts)] = true, true
		owned[i] = driver.CatalogReference{ID: ref.ID, Parts: append([]string(nil), ref.Parts...)}
	}
	limits, err := effectiveExecutionOptions(resolved.Source.Policy, ExecutionOptions{Timeout: 30 * time.Second, MaxRows: driver.MaxCatalogColumns, MaxBytes: driver.MaxCatalogBytes})
	if err != nil {
		return nil, executionError(ExecutionConfig, "catalog execution policy is invalid")
	}
	entry, err := r.entryFor(resolved)
	if err != nil {
		return nil, err
	}
	if err := entry.admission.acquire(ctx); err != nil {
		return nil, err
	}
	defer entry.admission.release()
	leaseCtx, leaseCancel := context.WithCancel(ctx)
	defer leaseCancel()
	remove := entry.trackLease(leaseCancel)
	defer remove()
	openCtx, openCancel := context.WithTimeout(leaseCtx, limits.Timeout)
	runtime, openErr := entry.readyRuntime(openCtx, r.secrets)
	if openErr != nil {
		contextErr := openCtx.Err()
		openCancel()
		if contextErr != nil {
			return nil, contextExecutionError(contextErr)
		}
		return nil, openErr
	}
	contextErr := openCtx.Err()
	openCancel()
	if contextErr != nil {
		return nil, contextExecutionError(contextErr)
	}
	columns, bytesUsed := int64(0), int64(0)
	for _, ref := range owned {
		if columns >= limits.MaxRows || bytesUsed >= limits.MaxBytes {
			return nil, executionError(ExecutionLimit, "catalog metadata limit exceeded")
		}
		opCtx, opCancel := context.WithTimeout(leaseCtx, limits.Timeout)
		// The adapter receives its own reference so mutation cannot redefine the
		// identity against which returned evidence is checked.
		driverRef := driver.CatalogReference{ID: ref.ID, Parts: append([]string(nil), ref.Parts...)}
		metadata, describeErr := describeCatalogRelation(opCtx, runtime, driverRef, driver.CatalogLimits{MaxColumns: int(limits.MaxRows - columns), MaxBytes: int(limits.MaxBytes - bytesUsed)})
		contextErr := opCtx.Err()
		opCancel()
		if contextErr != nil {
			return nil, contextExecutionError(contextErr)
		}
		if describeErr != nil {
			return nil, describeErr
		}
		if metadata.Reference.ID != ref.ID || !reflect.DeepEqual(metadata.Reference.Parts, ref.Parts) || metadata.Outcome != "found" || !metadata.ColumnsComplete || len(metadata.Columns) == 0 {
			return nil, executionError(ExecutionResultContract, "catalog returned incomplete or mismatched relation evidence")
		}
		columns += int64(len(metadata.Columns))
		encoded, marshalErr := json.Marshal(metadata)
		if marshalErr != nil {
			return nil, executionError(ExecutionResultContract, "catalog evidence cannot be encoded")
		}
		bytesUsed += int64(len(encoded))
		if columns > limits.MaxRows || bytesUsed > limits.MaxBytes {
			return nil, executionError(ExecutionLimit, "catalog metadata limit exceeded")
		}
		// Detach the returned evidence from driver-owned slices/pointers.
		var snapshot driver.CatalogRelation
		if json.Unmarshal(encoded, &snapshot) != nil {
			return nil, executionError(ExecutionResultContract, "catalog evidence cannot be copied")
		}
		result = append(result, snapshot)
	}
	return result, nil
}

func describeCatalogRelation(ctx context.Context, runtime driver.Runtime, ref driver.CatalogReference, limits driver.CatalogLimits) (result driver.CatalogRelation, err error) {
	executor, acquireErr := runtime.Acquire(ctx)
	if !isNilLike(executor) {
		defer func() {
			if closeErr := executor.Close(); closeErr != nil && err == nil {
				result = driver.CatalogRelation{}
				err = executionError(ExecutionCleanup, "catalog lease cleanup failed")
			}
		}()
	}
	if acquireErr != nil || isNilLike(executor) {
		return result, executionError(ExecutionOpen, "catalog lease could not be acquired")
	}
	inspector, ok := executor.(driver.CatalogInspector)
	if !ok || isNilLike(inspector) {
		return result, executionError(ExecutionConfig, "DataSource does not support catalog inspection")
	}
	result, err = inspector.DescribeRelation(ctx, ref, limits)
	if err != nil {
		return driver.CatalogRelation{}, executionError(ExecutionDriver, "relation metadata is unavailable; inspect database permissions and relation identity")
	}
	return result, nil
}
