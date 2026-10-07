package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
)

func TestCatalogAuthorizationPrecedesAllConfiguration(t *testing.T) {
	ctx := context.Background()
	deny := semantic.ProjectAuthorizerFunc(func(_ context.Context, req semantic.ProjectAuthorizationRequest) semantic.ProjectAuthorizationDecision {
		if req.ProjectID != "sales" || req.Action != semantic.ProjectActionAuthor {
			t.Fatalf("request=%#v", req)
		}
		return semantic.ProjectAuthorizationDecision{Effect: semantic.ProjectAuthorizationDeny, Reason: semantic.ProjectAuthorizationReasonPolicyDenied}
	})
	for _, path := range []string{"missing-path", filepath.Join(t.TempDir(), "secret-config")} {
		_, err := bootstrap.LoadCatalogRunner(ctx, path, "sales", "warehouse", func(context.Context, string, string) bool {
			t.Fatal("host metadata policy ran before Project authorization")
			return true
		}, bootstrap.WithProjectAuthorizer(deny))
		if err == nil || strings.Contains(err.Error(), "configuration") {
			t.Fatalf("wrong denial=%v", err)
		}
	}
	for _, access := range []bootstrap.CatalogAccessAuthorizer{nil, func(context.Context, string, string) bool { return false }} {
		_, err := bootstrap.LoadCatalogRunner(ctx, "missing-path", "sales", "warehouse", access, bootstrap.WithLocalAllAccessProjectAuthorization())
		if err == nil || !strings.Contains(err.Error(), "metadata access denied") {
			t.Fatalf("metadata policy bypass=%v", err)
		}
	}
	if _, err := bootstrap.LoadCatalogRunner(ctx, "missing-path", "sales", "warehouse", bootstrap.LocalCatalogAccess); err == nil {
		t.Fatal("default authorizer allowed missing principal")
	}
}

func TestCatalogAssemblyNeedsNoSemanticProjectOrSecrets(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "metis.yaml")
	if err := os.WriteFile(root, []byte("version: 1\nprojects:\n  sales:\n    path: not-created/project.yaml\n    data_source: warehouse\ndata_sources:\n  path: datasources.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(dir, "datasources.yaml")
	if err := os.WriteFile(registry, []byte("warehouse:\n  type: doris\n  config:\n    host: ${CATALOG_MISSING_HOST}\n    password: ${CATALOG_MISSING_PASSWORD}\n  policy:\n    max_rows: 40\n    max_bytes: 10000\n    query_timeout: 1s\nunused:\n  type: unrelated\n  config: {}\n  policy:\n    max_rows: 40\n    max_bytes: 10000\n    query_timeout: 1s\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := bootstrap.LoadCatalogRunner(context.Background(), root, "sales", "warehouse", bootstrap.LocalCatalogAccess, bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithBackendRegistry(placementBackends(t)), bootstrap.WithExecutionCeilings(bootstrap.ExecutionCeilings{MaxRows: 5, MaxBytes: 500, QueryTimeout: 50 * time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	resolved, err := r.ResolveDataSource("warehouse")
	if err != nil || *resolved.Source.Policy.MaxRows != 5 || *resolved.Source.Policy.MaxBytes != 500 || resolved.Source.Policy.QueryTimeout != "50ms" {
		t.Fatalf("route=%#v err=%v", resolved, err)
	}
	if _, err := r.ResolveDataSource("unused"); err == nil {
		t.Fatal("unselected source became executable")
	}
	// A source not applied to this project is rejected before opening its registry.
	if err := os.Remove(registry); err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.LoadCatalogRunner(context.Background(), root, "sales", "unused", bootstrap.LocalCatalogAccess, bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithBackendRegistry(placementBackends(t)))
	if err == nil || !strings.Contains(err.Error(), "not applied") {
		t.Fatalf("unapplied selection read registry: %v", err)
	}
	if err := os.WriteFile(root, []byte("projects:\n  sales:\n    path: x\n---\npassword: SECRET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.LoadCatalogRunner(context.Background(), root, "sales", "warehouse", bootstrap.LocalCatalogAccess, bootstrap.WithLocalAllAccessProjectAuthorization())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("config leaked: %v", err)
	}
}
