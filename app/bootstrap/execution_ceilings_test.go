package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/bootstrap"
)

func TestExecutionCeilingsOnlyTightenPrivateRuntime(t *testing.T) {
	for _, tc := range []struct {
		name        string
		limits      bootstrap.ExecutionCeilings
		rows, bytes int64
		timeout     string
	}{
		{"tighter", bootstrap.ExecutionCeilings{MaxRows: 5, MaxBytes: 512, QueryTimeout: 500 * time.Millisecond}, 5, 512, "500ms"},
		{"looser", bootstrap.ExecutionCeilings{MaxRows: 1000, MaxBytes: 100000, QueryTimeout: 30 * time.Second}, 10, 1024, "1s"},
		{"unchanged", bootstrap.ExecutionCeilings{}, 10, 1024, "1s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := multiSourceInput(t, true)
			r, err := bootstrap.NewRuntime(context.Background(), input, bootstrap.WithBackendRegistry(placementBackends(t)), bootstrap.WithExecutionCeilings(tc.limits))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close(context.Background())
			for _, name := range r.DataSources.Names() {
				actual, err := r.DataSources.Resolve(name)
				if err != nil {
					t.Fatal(err)
				}
				if *actual.Policy.MaxRows != tc.rows || *actual.Policy.MaxBytes != tc.bytes || actual.Policy.QueryTimeout != tc.timeout {
					t.Fatalf("limits: %#v", actual.Policy)
				}
				original := input.DataSources[name].Policy
				if *original.MaxRows != 10 || *original.MaxBytes != 1024 || original.QueryTimeout != "1s" {
					t.Fatal("caller policy mutated")
				}
			}
		})
	}
	if r, err := bootstrap.NewRuntime(context.Background(), memoryInput(t), bootstrap.WithExecutionCeilings(bootstrap.ExecutionCeilings{MaxRows: -1})); err == nil {
		r.Close(context.Background())
		t.Fatal("accepted negative ceiling")
	}
}
