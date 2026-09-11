// Package driver defines the public execution SPI implemented by warehouse
// integrations. It deliberately excludes DataSource policy and routing state.
package driver

import (
	"context"
	"fmt"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/datasource"
)

// Factory opens one process-scoped Runtime for a configured DataSource type.
// ValidateConfig and OpenDataSource receive only driver-owned Config snapshots;
// they never receive admission, timeout, row, byte, or routing policy.
type Factory interface {
	DataSourceType() datasource.Type
	ValidateConfig(map[string]string) error
	OpenDataSource(context.Context, OpenRequest) (Runtime, error)
}

// OpenRequest is the only configuration representation passed to a Driver.
// Config is an owned flat string map with external references preserved.
// Resolved values are available only through Secrets by exact SecretRef lookup
// and never enter Config.
type OpenRequest struct {
	Config  map[string]string
	Secrets Secrets
}

// Secrets exposes resolved material only through exact SecretRef lookup. It
// intentionally has no iteration, formatting, or map-access surface.
type Secrets interface {
	Value(datasource.SecretRef) (string, bool)
}

// ConfigValue returns one literal Config value or resolves its exact external
// value reference without mutating Config. Drivers use it only while
// constructing their private connection parameters.
func ConfigValue(config map[string]string, secrets Secrets, name string) (string, bool, error) {
	configured, exists := config[name]
	if !exists {
		return "", false, nil
	}
	reference, isReference, err := datasource.ParseSecretRef(configured)
	if err != nil {
		return "", true, fmt.Errorf("config %q contains an invalid external value reference", name)
	}
	if !isReference {
		return configured, true, nil
	}
	if secrets == nil {
		return "", true, fmt.Errorf("config %q external value reference is unresolved", name)
	}
	value, found := secrets.Value(reference)
	if !found || value == "" {
		return "", true, fmt.Errorf("config %q external value reference is unresolved", name)
	}
	return value, true, nil
}

// Runtime owns one process-scoped live resource such as a pool or SDK client.
// Close must honor ctx cancellation/deadline or otherwise provide bounded
// cleanup semantics.
type Runtime interface {
	Acquire(context.Context) (Executor, error)
	Close(context.Context) error
}

// Executor is one request-scoped execution lease. Closing it must not close
// the shared Runtime resource.
type Executor interface {
	Execute(context.Context, *artifact.CompiledQuery) (ResultStream, error)
	Close() error
}

// ResultStream yields driver-decoded rows. Runtime owns normalization, limits,
// complete-result atomicity, and cleanup.
type ResultStream interface {
	Next(context.Context) ([]any, error)
	Close() error
}
