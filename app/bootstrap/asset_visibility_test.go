package bootstrap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
)

func TestRuntimeAssetVisibilityConfiguration(t *testing.T) {
	var nilPointer *service.AllVisibleAssetPolicy
	var nilFunction service.AssetVisibilityPolicyFunc
	for _, constructor := range []string{"file", "memory"} {
		for _, tc := range []struct {
			name    string
			options []bootstrap.RuntimeOption
			invalid bool
			hidden  bool
		}{
			{name: "omitted"},
			{name: "explicit-visible", options: []bootstrap.RuntimeOption{bootstrap.WithAssetVisibilityPolicy(service.AllVisibleAssetPolicy{})}},
			{name: "explicit-nil", options: []bootstrap.RuntimeOption{bootstrap.WithAssetVisibilityPolicy(nil)}, invalid: true},
			{name: "typed-nil-pointer", options: []bootstrap.RuntimeOption{bootstrap.WithAssetVisibilityPolicy(nilPointer)}, invalid: true},
			{name: "typed-nil-function", options: []bootstrap.RuntimeOption{bootstrap.WithAssetVisibilityPolicy(nilFunction)}, invalid: true},
			{name: "explicit-hidden", options: []bootstrap.RuntimeOption{bootstrap.WithAssetVisibilityPolicy(service.AssetVisibilityPolicyFunc(func(context.Context, service.AssetVisibilityRequest) service.AssetVisibilityDecision {
				return service.AssetVisibilityDecision{Effect: service.AssetVisibilityHidden}
			}))}, hidden: true},
		} {
			t.Run(constructor+"/"+tc.name, func(t *testing.T) {
				options := append([]bootstrap.RuntimeOption{bootstrap.WithLocalAllAccessProjectAuthorization()}, tc.options...)
				var r *bootstrap.Runtime
				var err error
				if constructor == "file" {
					r, err = bootstrap.LoadRuntime("../../examples/demo/metis.yaml", options...)
				} else {
					r, err = bootstrap.NewRuntime(context.Background(), memoryInput(t), options...)
				}
				if r != nil {
					t.Cleanup(func() { _ = r.Close(context.Background()) })
				}
				if tc.invalid {
					var typed *serrors.Error
					if r != nil || !errors.As(err, &typed) || typed.Code != serrors.ErrInvalidExecutionConfig {
						t.Fatalf("explicit nil policy must reject construction: runtime=%v err=%v", r != nil, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, discovery := range []*service.DiscoveryService{r.Discovery, r.Current("demo").Discovery} {
					_, err = discovery.GetModel(context.Background(), service.GetModelRequest{Project: "demo", Model: "sales"})
					if (err != nil) != tc.hidden {
						t.Fatalf("model access: hidden=%v err=%v", tc.hidden, err)
					}
				}
			})
		}
	}
}
