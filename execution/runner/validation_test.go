package runner_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
)

type validationLease struct {
	catalogLease
	probe func(context.Context, *artifact.CompiledQuery) (driver.ValidationEvidence, error)
}

func (e *validationLease) ValidateCompiled(ctx context.Context, c *artifact.CompiledQuery, _ driver.CatalogLimits) (driver.ValidationEvidence, error) {
	return e.probe(ctx, c)
}

func TestValidationLeaseNeverExecutesAndAlwaysCleansUp(t *testing.T) {
	for _, scenario := range []string{"accepted", "error", "timeout", "unsupported", "invalid", "cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			lease := &validationLease{}
			lease.probe = func(ctx context.Context, c *artifact.CompiledQuery) (driver.ValidationEvidence, error) {
				if c.SqlRenderResult.SQL != "SELECT 1" {
					t.Fatal("changed compiled SQL")
				}
				switch scenario {
				case "error":
					return driver.ValidationEvidence{}, errors.New("password=SECRET")
				case "timeout":
					<-ctx.Done()
					return driver.ValidationEvidence{}, ctx.Err()
				case "invalid":
					return driver.ValidationEvidence{Outcome: "invented"}, nil
				case "cleanup":
					lease.closeErr = errors.New("SECRET")
				case "unsupported":
					return driver.ValidationEvidence{Outcome: "unsupported"}, nil
				}
				return driver.ValidationEvidence{Outcome: "accepted", Method: "explain"}, nil
			}
			r := newExecutionRuntime(t, &staticRuntimeFactory{runtime: &acquireFuncRuntime{acquire: func(context.Context) (driver.Executor, error) { return lease, nil }}}, testSecretResolver{value: "SECRET"}, nil, policy(time.Second, 100, 10000))
			defer r.Close(context.Background())
			route, err := r.ResolveDataSource("doris-prod")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			result, err := r.ValidateCompiled(ctx, route, compiledSQL("SELECT 1"))
			if !lease.closed {
				t.Fatal("lease leaked")
			}
			if scenario == "accepted" || scenario == "unsupported" {
				if err != nil || result.Outcome != scenario {
					t.Fatalf("evidence=%+v err=%v", result, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unsanitized failure=%v", err)
			}
		})
	}
}

func TestValidationMissingCapabilityDoesNotExecute(t *testing.T) {
	lease := &catalogLease{}
	r := catalogRuntime(t, lease, 100)
	defer r.Close(context.Background())
	route, _ := r.ResolveDataSource("doris-prod")
	evidence, err := r.ValidateCompiled(context.Background(), route, compiledSQL("SELECT 1"))
	if err != nil || evidence.Outcome != "unsupported" || !lease.closed {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}
