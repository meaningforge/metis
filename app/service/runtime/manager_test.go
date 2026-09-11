package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	runtime "github.com/meaningforge/metis/app/service/runtime"
	semantic "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/serrors"
)

func TestReplacementPinsWholeRequestsAndRejectsStaleOrWrongScope(t *testing.T) {
	r, err := bootstrap.LoadRuntime("../../../examples/demo/metis.yaml")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := source.LoadProject("demo", "../../../examples/demo/project.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{SubjectID: "host", Scopes: []string{auth.ScopeSemanticActivate}})
	pinned := r.Generations.Pin(ctx)
	first, ok, err := runtime.FromContext(pinned, "demo")
	if err != nil || !ok {
		t.Fatal(err)
	}
	next, err := r.Generations.Replace(ctx, "demo", loaded.Manifest, loaded.Bundle.ContentDigest, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	still, _, err := runtime.FromContext(r.Generations.Pin(pinned), "demo")
	if err != nil || still != first || next.ID != first.ID+1 {
		t.Fatal("request snapshot changed")
	}
	fresh, _, err := runtime.FromContext(r.Generations.Pin(ctx), "demo")
	if err != nil || fresh != next {
		t.Fatal("new request did not see replacement")
	}
	if _, err := r.Generations.Replace(ctx, "demo", loaded.Manifest, loaded.Bundle.ContentDigest, first.ID); err == nil {
		t.Fatal("stale snapshot accepted")
	}
	wrong, err := source.LoadProject("other", "../../../examples/demo/project.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Generations.Replace(ctx, "demo", wrong.Manifest, wrong.Bundle.ContentDigest, next.ID); err == nil {
		t.Fatal("cross-project install accepted")
	}
	if _, err := r.Generations.Replace(context.Background(), "demo", loaded.Manifest, loaded.Bundle.ContentDigest, next.ID); err == nil {
		t.Fatal("unauthenticated replacement accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.Generations.Replace(cancelled, "demo", loaded.Manifest, loaded.Bundle.ContentDigest, next.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if r.Current("demo") != next {
		t.Fatal("failed replacement changed current state")
	}
}

func TestConcurrentReplacementHasOneWinner(t *testing.T) {
	r, err := bootstrap.LoadRuntime("../../../examples/demo/metis.yaml", bootstrap.WithProjectAuthorizer(semantic.AllAccessProjectAuthorizer{}))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := source.LoadProject("demo", "../../../examples/demo/project.yaml")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := r.Generations.Replace(context.Background(), "demo", loaded.Manifest, loaded.Bundle.ContentDigest, 1)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else {
			var e *serrors.Error
			if !errors.As(err, &e) || e.Code != serrors.ErrSemanticGenerationConflict {
				t.Fatal(err)
			}
		}
	}
	if wins != 1 || r.Current("demo").ID != 2 {
		t.Fatalf("wins=%d current=%d", wins, r.Current("demo").ID)
	}
}

func TestPinRebindsContextsFromAnotherManager(t *testing.T) {
	for _, captured := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent-captured=%t", captured), func(t *testing.T) {
			a, err := bootstrap.LoadRuntime("../../../examples/demo/metis.yaml")
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close(context.Background())
			b, err := bootstrap.LoadRuntime("../../../examples/demo/metis.yaml")
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close(context.Background())
			type requestKey struct{}
			base, cancel := context.WithCancel(context.WithValue(context.Background(), requestKey{}, "request-value"))
			defer cancel()
			parent := a.Generations.Pin(base)
			if captured {
				if _, ok, err := runtime.FromContext(parent, "demo"); !ok || err != nil {
					t.Fatalf("capture A: ok=%v err=%v", ok, err)
				}
			}
			child := b.Generations.Pin(parent)
			generation, ok, err := runtime.FromContext(child, "demo")
			if err != nil || !ok || generation != b.Current("demo") {
				t.Fatalf("B.Pin must resolve B's generation: ok=%v err=%v", ok, err)
			}
			original, ok, err := runtime.FromContext(parent, "demo")
			if err != nil || !ok || original != a.Current("demo") {
				t.Fatal("rebinding changed the parent scope")
			}
			if b.Generations.Pin(child) != child {
				t.Fatal("same-manager pin must preserve the scope")
			}
			if child.Value(requestKey{}) != "request-value" {
				t.Fatal("rebinding lost context values")
			}
			cancel()
			if !errors.Is(child.Err(), context.Canceled) {
				t.Fatal("rebinding lost cancellation")
			}
		})
	}
}
