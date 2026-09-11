package runner_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
)

func TestEnvSecretResolverFailsClosed(t *testing.T) {
	resolver := runner.NewEnvSecretResolver()
	if _, err := resolver.ResolveSecret(context.Background(), datasource.SecretRef{Provider: "missing", Key: "NOPE"}); err == nil {
		t.Fatal("unsupported secret provider was accepted")
	}
}
