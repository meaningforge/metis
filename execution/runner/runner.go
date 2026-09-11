// Package runner owns bounded physical execution after compilation. It is
// independent of semantic manifests, planning, renderers, and transports.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/renderer/sql"
)

// SecretResolver resolves one provider-owned SecretRef immediately before an
// execution resource is opened. A deployment implementation routes by
// SecretRef.Provider and must fail closed for providers it does not own.
type SecretResolver interface {
	ResolveSecret(context.Context, datasource.SecretRef) (string, error)
}

// resolvedSecrets is initialization-attempt material passed only through the
// public driver.Secrets lookup interface. Runner does not retain it after the
// DriverFactory open call returns.
type resolvedSecrets struct {
	values map[datasource.SecretRef]string
}

func (s resolvedSecrets) Value(reference datasource.SecretRef) (string, bool) {
	value, exists := s.values[reference]
	return value, exists
}

// ExecutionOptions optionally tightens deployment policy. Zero values leave
// the corresponding deployment ceiling unchanged.
type ExecutionOptions struct {
	Timeout  time.Duration
	MaxRows  int64
	MaxBytes int64
}

// ResultSet is a complete bounded normalized result. A limit breach never
// returns a partially populated ResultSet.
type ResultSet struct {
	Schema artifact.OutputSchema `json:"schema"`
	Rows   [][]any               `json:"rows"`
	Count  int64                 `json:"count"`
	Bytes  int64                 `json:"bytes"`
}

// ExecutionErrorCode is the closed internal execution failure vocabulary.
type ExecutionErrorCode string

const (
	ExecutionInvalidInput   ExecutionErrorCode = "INVALID_INPUT"
	ExecutionConfig         ExecutionErrorCode = "INVALID_CONFIGURATION"
	ExecutionSecret         ExecutionErrorCode = "SECRET_RESOLUTION_FAILED"
	ExecutionOpen           ExecutionErrorCode = "EXECUTOR_OPEN_FAILED"
	ExecutionDriver         ExecutionErrorCode = "EXECUTOR_FAILED"
	ExecutionResultContract ExecutionErrorCode = "RESULT_CONTRACT_MISMATCH"
	ExecutionLimit          ExecutionErrorCode = "EXECUTION_LIMIT_EXCEEDED"
	ExecutionCapacity       ExecutionErrorCode = "EXECUTION_CAPACITY_EXCEEDED"
	ExecutionTimeout        ExecutionErrorCode = "EXECUTION_TIMEOUT"
	ExecutionCancelled      ExecutionErrorCode = "EXECUTION_CANCELLED"
	ExecutionCleanup        ExecutionErrorCode = "EXECUTION_CLEANUP_FAILED"
	ExecutionClosed         ExecutionErrorCode = "EXECUTION_RUNTIME_CLOSED"
)

// ExecutionError is a redacted structured runtime failure. It deliberately
// does not retain driver or secret-provider errors, which may contain secrets.
type ExecutionError struct {
	Code           ExecutionErrorCode
	Message        string
	ResultContract *ResultContractFailure
}

// ResultContractFailure is the bounded, value-free description of one
// OutputSchema normalization mismatch.
type ResultContractFailure struct {
	Stage            string
	Column           string
	ExpectedDatatype string
	ObservedFamily   ResultValueFamily
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

// ExecutionObservation is a bounded, secret-free lifecycle event. Its
// observer is optional and cannot affect execution semantics.
type ExecutionObservation struct {
	QueryID        string
	DataSourceType datasource.Type
	Code           ExecutionErrorCode
	Duration       time.Duration
	Rows           int64
	Bytes          int64
}

type queryIDContextKey struct{}

// WithQueryID attaches an opaque application correlation identifier to the
// execution context. It is observation metadata only, never an execution key.
func WithQueryID(ctx context.Context, queryID string) context.Context {
	if ctx == nil || queryID == "" {
		return ctx
	}
	return context.WithValue(ctx, queryIDContextKey{}, queryID)
}

// QueryIDFromContext returns the opaque correlation identifier, if any.
func QueryIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	queryID, _ := ctx.Value(queryIDContextKey{}).(string)
	return queryID
}

// ExecutionObserver receives a completed execution observation.
type ExecutionObserver interface {
	ObserveExecution(context.Context, ExecutionObservation)
}

// Runner composes DataSource instances, type-level Backends, and
// secret resolution. It owns neither semantic resolution nor Agent transport.
type Runner struct {
	sources  *datasource.DataSourceRegistry
	backends *backend.BackendRegistry
	secrets  SecretResolver
	observer ExecutionObserver

	mu      sync.Mutex
	entries map[string]*dataSourceEntry
	closed  bool
}

// ResolvedDataSource is the single runtime-owned route from a deployment
// DataSource instance to its type-level Backend. It is transient execution
// state, never semantic configuration or an Agent-facing contract.
type ResolvedDataSource struct {
	Name    string
	Source  datasource.DataSource
	Backend backend.Backend
}

// New creates the internal query-running boundary consumed only by the
// application-layer query_metrics service.
func New(sources *datasource.DataSourceRegistry, backends *backend.BackendRegistry, secrets SecretResolver, observer ExecutionObserver) *Runner {
	if sources != nil {
		sources.Freeze()
	}
	if backends != nil {
		backends.Freeze()
	}
	return &Runner{sources: sources, backends: backends, secrets: secrets, observer: observer, entries: make(map[string]*dataSourceEntry)}
}

// WithObserver installs startup-time execution observation. Callers configure
// it before serving traffic; observation remains optional and cannot affect
// execution semantics.
func (r *Runner) WithObserver(observer ExecutionObserver) *Runner {
	if r != nil {
		r.observer = observer
	}
	return r
}

// Close stops admission for every DataSource, drains all active request work
// within the shared ctx, cancels all remaining leases if draining expires, and
// then starts bounded cleanup for every initialized process-scoped resource.
func (r *Runner) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	entries := make([]*dataSourceEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	r.mu.Unlock()
	for _, entry := range entries {
		entry.admission.stop()
	}
	drainErr := forEachDataSourceEntry(entries, func(entry *dataSourceEntry) error {
		return entry.admission.drain(ctx)
	})
	if drainErr != nil {
		for _, entry := range entries {
			entry.cancelLeases()
		}
	}
	cleanupErr := forEachDataSourceEntry(entries, func(entry *dataSourceEntry) error {
		return entry.closeResource(ctx)
	})
	return errors.Join(drainErr, cleanupErr)
}

// RecycleDataSource closes the initialized process-scoped resource for one
// DataSource without closing the Runner. Callers must stop new work for that
// DataSource while recycling it. A later execution reinitializes the resource
// from the same frozen DataSource and Backend registrations.
func (r *Runner) RecycleDataSource(ctx context.Context, name string) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return executionError(ExecutionClosed, "execution runtime is closed")
	}
	entry, exists := r.entries[name]
	if exists {
		delete(r.entries, name)
	}
	r.mu.Unlock()
	if !exists {
		return nil
	}
	entry.admission.stop()
	if err := entry.admission.drain(ctx); err != nil {
		entry.cancelLeases()
		return errors.Join(err, entry.closeResource(ctx))
	}
	return entry.closeResource(ctx)
}

func forEachDataSourceEntry(entries []*dataSourceEntry, operation func(*dataSourceEntry) error) error {
	results := make(chan error, len(entries))
	for _, entry := range entries {
		go func() {
			results <- operation(entry)
		}()
	}
	var result error
	for range entries {
		result = errors.Join(result, <-results)
	}
	return result
}

// ResolveDataSource resolves one concrete DataSource and its Backend exactly
// once. Callers that need the Backend Renderer for compilation must pass this
// value unchanged to ExecuteResolved, so compilation and execution share one
// placement decision.
func (r *Runner) ResolveDataSource(name string) (ResolvedDataSource, error) {
	if r == nil || r.sources == nil || r.backends == nil {
		return ResolvedDataSource{}, executionError(ExecutionConfig, "execution runtime is not configured")
	}
	source, err := r.sources.Resolve(name)
	if err != nil {
		return ResolvedDataSource{}, executionError(ExecutionConfig, "DataSource is not configured")
	}
	backend, err := r.backends.Resolve(source.Type)
	if err != nil {
		return ResolvedDataSource{}, executionError(ExecutionConfig, "DataSource Backend is not configured")
	}
	return ResolvedDataSource{Name: name, Source: source, Backend: backend}, nil
}

func (r *Runner) entryFor(resolved ResolvedDataSource) (*dataSourceEntry, error) {
	if r == nil {
		return nil, executionError(ExecutionConfig, "execution runtime is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, executionError(ExecutionClosed, "execution runtime is closed")
	}
	if entry, exists := r.entries[resolved.Name]; exists {
		return entry, nil
	}
	entry, err := newDataSourceEntry(resolved.Source, resolved.Backend)
	if err != nil {
		return nil, executionError(ExecutionConfig, "DataSource execution policy is incomplete or invalid")
	}
	r.entries[resolved.Name] = entry
	return entry, nil
}

// Execute opens one DataSource-selected Executor and returns only a complete,
// bounded ResultSet. Caller options may tighten but never relax policy limits.
func (r *Runner) Execute(ctx context.Context, dataSource string, compiled *artifact.CompiledQuery, caller ExecutionOptions) (result ResultSet, err error) {
	resolved, err := r.ResolveDataSource(dataSource)
	if err != nil {
		return ResultSet{}, err
	}
	return r.ExecuteResolved(ctx, resolved, compiled, caller)
}

// ExecuteResolved executes through an already resolved route. It intentionally
// accepts no semantic state, renderer registry, or DataSource name lookup.
func (r *Runner) ExecuteResolved(ctx context.Context, resolved ResolvedDataSource, compiled *artifact.CompiledQuery, caller ExecutionOptions) (result ResultSet, err error) {
	started := time.Now()
	sourceType := resolved.Source.Type
	defer func() {
		r.observe(ctx, ExecutionObservation{QueryID: QueryIDFromContext(ctx), DataSourceType: sourceType, Code: executionErrorCode(err), Duration: time.Since(started), Rows: result.Count, Bytes: result.Bytes})
	}()

	if r == nil || r.sources == nil || r.backends == nil {
		return ResultSet{}, executionError(ExecutionConfig, "execution runtime is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return ResultSet{}, contextExecutionError(contextErr)
	}
	compiledSnapshot, snapshotErr := artifact.SnapshotCompiledQuery(compiled)
	if snapshotErr != nil {
		return ResultSet{}, executionError(ExecutionInvalidInput, "compiled query contains an unsupported value")
	}
	if compiledSnapshot == nil || compiledSnapshot.PhysicalQuery.SQL == "" {
		return ResultSet{}, executionError(ExecutionInvalidInput, "compiled query is required")
	}
	if len(compiledSnapshot.OutputSchema.Columns) == 0 {
		return ResultSet{}, executionError(ExecutionInvalidInput, "compiled query output schema is required")
	}
	executorCompiled, snapshotErr := artifact.SnapshotCompiledQuery(compiledSnapshot)
	if snapshotErr != nil {
		return ResultSet{}, executionError(ExecutionInvalidInput, "compiled query contains an unsupported value")
	}
	if err := validateCallerOptions(caller); err != nil {
		return ResultSet{}, executionError(ExecutionInvalidInput, "execution options are invalid")
	}

	source := resolved.Source
	backend := resolved.Backend
	if resolved.Name == "" || sourceType == "" || backend.Type == "" {
		return ResultSet{}, executionError(ExecutionConfig, "resolved DataSource route is incomplete")
	}
	if err := validateQueryDialect(compiledSnapshot.PhysicalQuery, backend); err != nil {
		return ResultSet{}, err
	}
	limits, limitErr := effectiveExecutionOptions(source.Policy, caller)
	if limitErr != nil {
		return ResultSet{}, executionError(ExecutionConfig, "DataSource execution policy is incomplete or invalid")
	}
	entry, entryErr := r.entryFor(resolved)
	if entryErr != nil {
		return ResultSet{}, entryErr
	}
	if admissionErr := entry.admission.acquire(ctx); admissionErr != nil {
		return ResultSet{}, admissionErr
	}
	defer entry.admission.release()
	executionContext, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	removeLease := entry.trackLease(cancel)
	defer removeLease()
	dataSourceRuntime, runtimeErr := entry.readyRuntime(executionContext, r.secrets)
	if runtimeErr != nil {
		if contextErr := executionContext.Err(); contextErr != nil {
			return ResultSet{}, contextExecutionError(contextErr)
		}
		return ResultSet{}, runtimeErr
	}
	executor, openErr := dataSourceRuntime.Acquire(executionContext)
	if !isNilLike(executor) {
		defer func() {
			if closeErr := executor.Close(); closeErr != nil && err == nil {
				result = ResultSet{}
				err = executionError(ExecutionCleanup, "Executor cleanup failed")
			}
		}()
	}
	if openErr != nil || isNilLike(executor) {
		if contextErr := executionContext.Err(); contextErr != nil {
			return ResultSet{}, contextExecutionError(contextErr)
		}
		return ResultSet{}, executionError(ExecutionOpen, "DriverFactory could not open an Executor")
	}

	stream, executeErr := executor.Execute(executionContext, executorCompiled)
	if !isNilLike(stream) {
		defer func() {
			if closeErr := stream.Close(); closeErr != nil && err == nil {
				result = ResultSet{}
				err = executionError(ExecutionCleanup, "result stream cleanup failed")
			}
		}()
	}
	if executeErr != nil || isNilLike(stream) {
		if contextErr := executionContext.Err(); contextErr != nil {
			return ResultSet{}, contextExecutionError(contextErr)
		}
		return ResultSet{}, executionError(ExecutionDriver, "Executor could not execute the physical query")
	}

	// compiledSnapshot is Runtime-owned and not shared with Executor, so the
	// returned ResultSet takes exclusive ownership of this schema copy.
	result.Schema = compiledSnapshot.OutputSchema
	for {
		row, nextErr := stream.Next(executionContext)
		if contextErr := executionContext.Err(); contextErr != nil {
			return ResultSet{}, contextExecutionError(contextErr)
		}
		if nextErr == io.EOF {
			return result, nil
		}
		if nextErr != nil {
			return ResultSet{}, executionError(ExecutionDriver, "Executor result stream failed")
		}
		if len(row) != len(compiledSnapshot.OutputSchema.Columns) {
			return ResultSet{}, executionError(ExecutionDriver, "Executor result row does not match output schema")
		}
		ownedRow, normalizeErr := normalizeResultRow(row, compiledSnapshot.OutputSchema)
		if normalizeErr != nil {
			var contractErr *ResultContractError
			if errors.As(normalizeErr, &contractErr) {
				return ResultSet{}, &ExecutionError{
					Code:    ExecutionResultContract,
					Message: "Executor result row does not satisfy output schema",
					ResultContract: &ResultContractFailure{
						Stage:            "result_normalization",
						Column:           contractErr.Column,
						ExpectedDatatype: string(contractErr.ExpectedDatatype),
						ObservedFamily:   contractErr.ObservedFamily,
					},
				}
			}
			return ResultSet{}, executionError(ExecutionDriver, "Executor result row cannot be normalized to output schema")
		}
		encoded, marshalErr := json.Marshal(ownedRow)
		if marshalErr != nil {
			return ResultSet{}, executionError(ExecutionDriver, "Executor result row cannot be accounted for")
		}
		rowBytes := int64(len(encoded))
		if result.Count == limits.MaxRows {
			return ResultSet{}, executionError(ExecutionLimit, "execution row limit exceeded")
		}
		if rowBytes > limits.MaxBytes-result.Bytes {
			return ResultSet{}, executionError(ExecutionLimit, "execution byte limit exceeded")
		}
		result.Rows = append(result.Rows, ownedRow)
		result.Count++
		result.Bytes += rowBytes
	}
}

func resolveSecrets(ctx context.Context, config map[string]string, resolver SecretResolver) (driver.Secrets, error) {
	references, err := datasource.SecretReferences(config)
	if err != nil {
		return nil, executionError(ExecutionSecret, "DataSource contains an invalid external value reference")
	}
	if len(references) == 0 {
		return resolvedSecrets{values: map[datasource.SecretRef]string{}}, nil
	}
	if resolver == nil {
		return nil, executionError(ExecutionSecret, "SecretResolver is required")
	}
	values := make(map[datasource.SecretRef]string, len(references))
	for _, reference := range references {
		value, resolveErr := resolver.ResolveSecret(ctx, reference)
		if resolveErr != nil || value == "" {
			return nil, executionError(ExecutionSecret, "SecretResolver could not resolve a required secret")
		}
		values[reference] = value
	}
	return resolvedSecrets{values: values}, nil
}

func effectiveExecutionOptions(policy datasource.DataSourcePolicy, caller ExecutionOptions) (ExecutionOptions, error) {
	if err := datasource.ValidatePolicy(policy); err != nil || policy.QueryTimeout == "" || policy.MaxRows == nil || policy.MaxBytes == nil {
		return ExecutionOptions{}, fmt.Errorf("incomplete policy")
	}
	timeout, err := time.ParseDuration(policy.QueryTimeout)
	if err != nil {
		return ExecutionOptions{}, err
	}
	return ExecutionOptions{
		Timeout:  stricterDuration(timeout, caller.Timeout),
		MaxRows:  stricterLimit(*policy.MaxRows, caller.MaxRows),
		MaxBytes: stricterLimit(*policy.MaxBytes, caller.MaxBytes),
	}, nil
}

func validateCallerOptions(options ExecutionOptions) error {
	if options.Timeout < 0 || options.MaxRows < 0 || options.MaxBytes < 0 {
		return fmt.Errorf("negative execution option")
	}
	return nil
}

func stricterDuration(deployment, caller time.Duration) time.Duration {
	if caller > 0 && caller < deployment {
		return caller
	}
	return deployment
}

func stricterLimit(deployment, caller int64) int64 {
	if caller > 0 && caller < deployment {
		return caller
	}
	return deployment
}

func validateQueryDialect(query sql.SQLRenderResult, backend backend.Backend) error {
	if query.SQL == "" || query.Dialect != backend.SQLDialect() {
		return executionError(ExecutionInvalidInput, "physical query dialect does not match the DataSource Backend")
	}
	return nil
}

func contextExecutionError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return executionError(ExecutionTimeout, "execution deadline exceeded")
	}
	return executionError(ExecutionCancelled, "execution cancelled")
}

func executionError(code ExecutionErrorCode, message string) *ExecutionError {
	return &ExecutionError{Code: code, Message: message}
}

func executionErrorCode(err error) ExecutionErrorCode {
	var executionErr *ExecutionError
	if errors.As(err, &executionErr) {
		return executionErr.Code
	}
	if err != nil {
		return ExecutionDriver
	}
	return ""
}

func (r *Runner) observe(ctx context.Context, observation ExecutionObservation) {
	if r == nil || r.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	r.observer.ObserveExecution(ctx, observation)
}
