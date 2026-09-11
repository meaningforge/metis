package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

type initAttempt struct {
	done    chan struct{}
	runtime driver.Runtime
	err     error
}

type dataSourceEntry struct {
	source    datasource.DataSource
	backend   backend.Backend
	admission *admissionGate

	mu          sync.Mutex
	runtime     driver.Runtime
	attempt     *initAttempt
	closed      bool
	nextLeaseID uint64
	leases      map[uint64]context.CancelFunc
}

func newDataSourceEntry(source datasource.DataSource, backend backend.Backend) (*dataSourceEntry, error) {
	gate, err := newAdmissionGate(source.Policy)
	if err != nil {
		return nil, err
	}
	return &dataSourceEntry{source: source, backend: backend, admission: gate, leases: make(map[uint64]context.CancelFunc)}, nil
}

// readyRuntime coalesces concurrent first use into one attempt and publishes
// that attempt's result to every waiter. A failed result is not retained after
// the attempt completes, so only requests arriving later may start a retry.
func (e *dataSourceEntry) readyRuntime(ctx context.Context, secrets SecretResolver) (driver.Runtime, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, executionError(ExecutionClosed, "execution runtime is closed")
	}
	if !isNilLike(e.runtime) {
		runtime := e.runtime
		e.mu.Unlock()
		return runtime, nil
	}
	if e.attempt != nil {
		attempt := e.attempt
		e.mu.Unlock()
		select {
		case <-attempt.done:
			return attempt.runtime, attempt.err
		case <-ctx.Done():
			return nil, contextExecutionError(ctx.Err())
		}
	}
	attempt := &initAttempt{done: make(chan struct{})}
	e.attempt = attempt
	source := e.source
	backend := e.backend
	e.mu.Unlock()

	resolvedSecrets, err := resolveSecrets(ctx, source.Config, secrets)
	var runtime driver.Runtime
	if err == nil {
		if backend.DriverFactory == nil {
			err = executionError(ExecutionConfig, "DataSource Backend has no DriverFactory")
		} else {
			runtime, err = backend.DriverFactory.OpenDataSource(ctx, driver.OpenRequest{Config: source.ConfigSnapshot(), Secrets: resolvedSecrets})
			if err != nil || isNilLike(runtime) {
				err = executionError(ExecutionOpen, "DriverFactory could not open a DataSource runtime")
			}
		}
	}
	if err == nil && ctx.Err() != nil {
		err = contextExecutionError(ctx.Err())
	}
	if err != nil && !isNilLike(runtime) {
		_ = closeDataSourceRuntime(ctx, runtime)
	}

	e.mu.Lock()
	if err == nil && !e.closed {
		e.runtime = runtime
	}
	if err == nil && e.closed {
		err = executionError(ExecutionClosed, "execution runtime is closed")
	}
	if err == nil {
		attempt.runtime = runtime
	}
	attempt.err = err
	close(attempt.done)
	if e.attempt == attempt {
		e.attempt = nil
	}
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

func (e *dataSourceEntry) closeResource(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	runtime := e.runtime
	e.runtime = nil
	e.mu.Unlock()
	if isNilLike(runtime) {
		return nil
	}
	if err := closeDataSourceRuntime(ctx, runtime); err != nil {
		return executionError(ExecutionCleanup, "DataSource runtime cleanup failed")
	}
	return nil
}

func closeDataSourceRuntime(ctx context.Context, runtime driver.Runtime) error {
	if isNilLike(runtime) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	go func() {
		done <- runtime.Close(ctx)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// trackLease registers the request cancellation owned by Runtime shutdown.
// The returned function is idempotent so normal request cleanup can race with
// shutdown cancellation without affecting another request's lease.
func (e *dataSourceEntry) trackLease(cancel context.CancelFunc) func() {
	if cancel == nil {
		return func() {}
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		cancel()
		return func() {}
	}
	e.nextLeaseID++
	id := e.nextLeaseID
	e.leases[id] = cancel
	e.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Lock()
			delete(e.leases, id)
			e.mu.Unlock()
		})
	}
}

func (e *dataSourceEntry) cancelLeases() {
	e.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.leases))
	for _, cancel := range e.leases {
		cancels = append(cancels, cancel)
	}
	e.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// admissionGate bounds physical executions for one DataSource. It intentionally
// does not serialize query execution; synchronization covers state transitions
// only.
type admissionGate struct {
	maxConcurrency int
	maxQueue       int
	queueTimeout   time.Duration

	mu      sync.Mutex
	active  int
	closed  bool
	waiters []*admissionWaiter
	changed chan struct{}
}

type admissionWaiter struct {
	ready chan error
}

func newAdmissionGate(policy datasource.DataSourcePolicy) (*admissionGate, error) {
	policy = datasource.WithPolicyDefaults(policy)
	if err := datasource.ValidatePolicy(policy); err != nil {
		return nil, err
	}
	queueTimeout := time.Duration(0)
	if policy.MaxQueue != nil && *policy.MaxQueue > 0 {
		parsed, err := time.ParseDuration(policy.QueueTimeout)
		if err != nil {
			return nil, err
		}
		queueTimeout = parsed
	}
	return &admissionGate{
		maxConcurrency: *policy.MaxConcurrency,
		maxQueue:       valueOrZero(policy.MaxQueue),
		queueTimeout:   queueTimeout,
		changed:        make(chan struct{}),
	}, nil
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (g *admissionGate) acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return contextExecutionError(err)
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return executionError(ExecutionClosed, "execution runtime is closed")
	}
	// A new request must never bypass an older waiter, even when a release and
	// an acquisition race. This makes admission FIFO per DataSource.
	if g.active < g.maxConcurrency && len(g.waiters) == 0 {
		g.active++
		g.mu.Unlock()
		return nil
	}
	if g.maxQueue == 0 || len(g.waiters) >= g.maxQueue {
		g.mu.Unlock()
		return executionError(ExecutionCapacity, "DataSource execution capacity is exhausted")
	}
	waiter := &admissionWaiter{ready: make(chan error, 1)}
	g.waiters = append(g.waiters, waiter)
	g.mu.Unlock()

	timer := time.NewTimer(g.queueTimeout)
	defer timer.Stop()
	select {
	case err := <-waiter.ready:
		return err
	case <-ctx.Done():
		if g.removeWaiter(waiter) {
			return contextExecutionError(ctx.Err())
		}
		// A concurrent release already transferred a permit to this waiter.
		// Return success so the caller's deferred release owns that permit;
		// its next context check will still fail closed.
		return <-waiter.ready
	case <-timer.C:
		if g.removeWaiter(waiter) {
			return executionError(ExecutionCapacity, "DataSource execution capacity is exhausted")
		}
		return <-waiter.ready
	}
}

func (g *admissionGate) removeWaiter(waiter *admissionWaiter) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for index, queued := range g.waiters {
		if queued != waiter {
			continue
		}
		g.waiters = append(g.waiters[:index], g.waiters[index+1:]...)
		g.grantLocked()
		return true
	}
	return false
}

func (g *admissionGate) release() {
	g.mu.Lock()
	if g.active > 0 {
		g.active--
	}
	g.grantLocked()
	g.signalLocked()
	g.mu.Unlock()
}

func (g *admissionGate) stop() {
	g.mu.Lock()
	g.closed = true
	for _, waiter := range g.waiters {
		waiter.ready <- executionError(ExecutionClosed, "execution runtime is closed")
	}
	g.waiters = nil
	g.signalLocked()
	g.mu.Unlock()
}

func (g *admissionGate) drain(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		g.mu.Lock()
		if g.active == 0 {
			g.mu.Unlock()
			return nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return fmt.Errorf("drain execution capacity: %w", ctx.Err())
		}
	}
}

func (g *admissionGate) signalLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}

func (g *admissionGate) grantLocked() {
	for !g.closed && g.active < g.maxConcurrency && len(g.waiters) > 0 {
		waiter := g.waiters[0]
		g.waiters = g.waiters[1:]
		g.active++
		waiter.ready <- nil
	}
}
