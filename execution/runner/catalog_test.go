package runner_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	execution "github.com/meaningforge/metis/execution/runner"
)

type catalogLease struct {
	describe func(context.Context, driver.CatalogReference, driver.CatalogLimits) (driver.CatalogRelation, error)
	closed   bool
	closeErr error
}

func (*catalogLease) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	panic("catalog must never execute compiled SQL")
}
func (e *catalogLease) Close() error { e.closed = true; return e.closeErr }
func (e *catalogLease) DescribeRelation(ctx context.Context, ref driver.CatalogReference, limits driver.CatalogLimits) (driver.CatalogRelation, error) {
	return e.describe(ctx, ref, limits)
}
func catalogEvidence(ref driver.CatalogReference) driver.CatalogRelation {
	return driver.CatalogRelation{Reference: ref, Outcome: "found", ColumnsComplete: true, Columns: []driver.CatalogColumn{{Name: "Amount", NativeType: driver.CatalogNativeType{Name: "INT"}, Nullable: "not_null"}}}
}
func catalogRuntime(t *testing.T, lease *catalogLease, maxColumns int64) *execution.Runner {
	return newExecutionRuntime(t, &staticRuntimeFactory{runtime: &acquireFuncRuntime{acquire: func(context.Context) (driver.Executor, error) { return lease, nil }}}, testSecretResolver{value: "secret"}, nil, policy(time.Second, maxColumns, 10000))
}
func TestCatalogRunnerEvidenceAndCleanup(t *testing.T) {
	refs := []driver.CatalogReference{{ID: "orders", Parts: []string{"Analytics", "Orders"}}, {ID: "items", Parts: []string{"Analytics", "Items"}}}
	calls := 0
	lease := &catalogLease{describe: func(ctx context.Context, ref driver.CatalogReference, limits driver.CatalogLimits) (driver.CatalogRelation, error) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
			t.Fatal("deployment deadline not enforced")
		}
		if limits.MaxColumns != 2-calls {
			t.Fatalf("remaining columns=%d", limits.MaxColumns)
		}
		calls++
		return catalogEvidence(ref), nil
	}}
	r := catalogRuntime(t, lease, 2)
	defer r.Close(context.Background())
	result, err := r.DescribeRelations(context.Background(), "doris-prod", refs)
	if err != nil || len(result) != 2 || !lease.closed {
		t.Fatalf("result=%#v err=%v closed=%v", result, err, lease.closed)
	}
	refs[0].Parts[0] = "changed"
	if result[0].Reference.Parts[0] != "Analytics" {
		t.Fatal("caller changed evidence")
	}
}
func TestCatalogRunnerRejectsPartialAndUnsafeEvidence(t *testing.T) {
	ref := driver.CatalogReference{ID: "orders", Parts: []string{"Analytics", "Orders"}}
	for _, scenario := range []string{"error", "id", "parts", "mutation", "partial", "empty", "limit", "timeout", "cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			lease := &catalogLease{}
			lease.describe = func(ctx context.Context, ref driver.CatalogReference, _ driver.CatalogLimits) (driver.CatalogRelation, error) {
				m := catalogEvidence(ref)
				switch scenario {
				case "error":
					return driver.CatalogRelation{}, errors.New("secret password and endpoint")
				case "id":
					m.Reference.ID = "other"
				case "parts":
					m.Reference.Parts = []string{"other", "table"}
				case "mutation":
					ref.Parts[0] = "mutated"
					m.Reference = ref
				case "partial":
					m.ColumnsComplete = false
				case "empty":
					m.Columns = nil
				case "limit":
					m.Columns = append(m.Columns, m.Columns[0])
				case "timeout":
					<-ctx.Done()
					return m, ctx.Err()
				case "cleanup":
					lease.closeErr = errors.New("secret cleanup")
				}
				return m, nil
			}
			r := catalogRuntime(t, lease, 1)
			defer r.Close(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			result, err := r.DescribeRelations(ctx, "doris-prod", []driver.CatalogReference{ref})
			if err == nil || result != nil || !lease.closed || strings.Contains(err.Error(), "secret") {
				t.Fatalf("result=%#v err=%v closed=%v", result, err, lease.closed)
			}
		})
	}
}
func TestInvalidCatalogSelectorsDoNotResolveSecrets(t *testing.T) {
	for _, refs := range [][]driver.CatalogReference{nil, {{ID: "x", Parts: []string{"table"}}}, {{ID: "x", Parts: []string{"db", "*"}}}, {{ID: "x", Parts: []string{"db", "t` UNION SELECT secret"}}}, {{ID: "x", Parts: []string{"db", "table"}}, {ID: "x", Parts: []string{"db", "another"}}}, {{ID: "x", Parts: []string{"db", "table"}}, {ID: "y", Parts: []string{"db", "table"}}}} {
		resolver := testSecretResolver{resolve: func(context.Context, datasource.SecretRef) (string, error) {
			t.Fatal("invalid selectors resolved secrets")
			return "", nil
		}}
		r := newExecutionRuntime(t, &runtimeDriverFactory{}, resolver, nil, policy(time.Second, 10, 10000))
		_, err := r.DescribeRelations(context.Background(), "doris-prod", refs)
		assertExecutionCode(t, err, execution.ExecutionInvalidInput)
		r.Close(context.Background())
	}
}
