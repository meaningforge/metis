//go:build duckdb

package command

import (
	"context"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
)

type capturingDriverFactory struct {
	inner driver.Factory
	sink  *physicalQueryCapture
}

func (f capturingDriverFactory) DataSourceType() datasource.Type {
	return f.inner.DataSourceType()
}

func (f capturingDriverFactory) ValidateConfig(config map[string]string) error {
	return f.inner.ValidateConfig(config)
}

func (f capturingDriverFactory) OpenDataSource(ctx context.Context, request driver.OpenRequest) (driver.Runtime, error) {
	runtime, err := f.inner.OpenDataSource(ctx, request)
	if err != nil {
		return nil, err
	}
	return capturingDriverRuntime{inner: runtime, sink: f.sink}, nil
}

type capturingDriverRuntime struct {
	inner driver.Runtime
	sink  *physicalQueryCapture
}

func (r capturingDriverRuntime) Acquire(ctx context.Context) (driver.Executor, error) {
	executor, err := r.inner.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return capturingDriverExecutor{inner: executor, sink: r.sink}, nil
}

func (r capturingDriverRuntime) Close(ctx context.Context) error {
	return r.inner.Close(ctx)
}

type capturingDriverExecutor struct {
	inner driver.Executor
	sink  *physicalQueryCapture
}

func (e capturingDriverExecutor) Execute(ctx context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	if err := e.sink.record(runner.QueryIDFromContext(ctx), compiled); err != nil {
		return nil, err
	}
	return e.inner.Execute(ctx, compiled)
}

func (e capturingDriverExecutor) Close() error {
	return e.inner.Close()
}
