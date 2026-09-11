package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
)

var ErrUnauthorized = errors.New("unauthorized")

// APIKeyVerifier authenticates a bearer API key and returns a stable Principal.
// SaaS deployments can replace the env-backed implementation with an RDS-backed
// verifier without changing REST or MCP transports.
type APIKeyVerifier interface {
	Verify(ctx context.Context, token string) (*Principal, error)
}

type StaticAPIKeyVerifier struct {
	key string
}

func NewStaticAPIKeyVerifier(key string) (*StaticAPIKeyVerifier, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("API key is required")
	}
	return &StaticAPIKeyVerifier{key: key}, nil
}

func (v *StaticAPIKeyVerifier) Verify(ctx context.Context, token string) (*Principal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if v == nil || v.key == "" || token == "" || len(token) != len(v.key) || subtle.ConstantTimeCompare([]byte(token), []byte(v.key)) != 1 {
		return nil, ErrUnauthorized
	}
	return &Principal{
		TenantID:  "local",
		SubjectID: "local",
		APIKeyID:  "env",
		Scopes:    []string{ScopeAll},
	}, nil
}
