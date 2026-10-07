package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
)

func TestValidationAuthorizationBeforeConfiguration(t *testing.T) {
	for _, denied := range []semantic.ProjectAction{semantic.ProjectActionAuthor, semantic.ProjectActionCompile} {
		policy := semantic.ProjectAuthorizerFunc(func(_ context.Context, r semantic.ProjectAuthorizationRequest) semantic.ProjectAuthorizationDecision {
			if r.Action == denied {
				return semantic.ProjectAuthorizationDecision{Effect: semantic.ProjectAuthorizationDeny, Reason: semantic.ProjectAuthorizationReasonPolicyDenied}
			}
			return semantic.ProjectAuthorizationDecision{Effect: semantic.ProjectAuthorizationAllow, Reason: semantic.ProjectAuthorizationReasonAllAccess}
		})
		_, err := bootstrap.LoadValidationRuntime(context.Background(), "missing-secret-config", "sales", bootstrap.WithProjectAuthorizer(policy))
		if err == nil || strings.Contains(err.Error(), "configuration") {
			t.Fatalf("authorization ordering: %v", err)
		}
	}
}

func TestInvalidValidationCandidateDoesNotReadRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metis.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nprojects:\n  sales:\n    path: absent-project.yaml\n    data_source: warehouse\ndata_sources:\n  path: absent-secret-registry.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := bootstrap.LoadValidationRuntime(context.Background(), path, "sales", bootstrap.WithLocalAllAccessProjectAuthorization())
	if err == nil || !strings.Contains(err.Error(), "offline candidate") {
		t.Fatalf("read registry before source validation: %v", err)
	}
}
