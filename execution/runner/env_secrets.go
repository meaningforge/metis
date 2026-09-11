package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/meaningforge/metis/execution/datasource"
)

// EnvSecretResolver resolves the explicit env SecretRef provider. It never
// accepts a plaintext configuration fallback or exposes secret values in an
// error.
type EnvSecretResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvSecretResolver() *EnvSecretResolver {
	return &EnvSecretResolver{lookup: os.LookupEnv}
}

func (r *EnvSecretResolver) ResolveSecret(ctx context.Context, reference datasource.SecretRef) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r == nil || r.lookup == nil || reference.Provider != "env" || reference.Key == "" {
		return "", fmt.Errorf("secret reference cannot be resolved")
	}
	value, exists := r.lookup(reference.Key)
	if !exists || value == "" {
		return "", fmt.Errorf("secret reference cannot be resolved")
	}
	return value, nil
}
