package bootstrap_test

import (
	"context"
	"os"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
)

func memoryInput(t *testing.T) bootstrap.RuntimeInput {
	t.Helper()
	cfg, err := execution.LoadProjectConfig([]byte("semantic_sources:\n  sales:\n    path: models/*.yaml\n"))
	if err != nil {
		t.Fatal(err)
	}
	model, err := os.ReadFile("../../examples/demo/models/sales.ossie.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return bootstrap.RuntimeInput{Projects: map[string]bootstrap.ProjectInput{"demo": {Config: cfg, Documents: []source.SourceDocument{{Source: "sales", Path: "models/sales.yaml", Content: model}}}}}
}

func TestMemoryRuntimeOwnsInputWithoutReadingDocumentPaths(t *testing.T) {
	input := memoryInput(t)
	r, err := bootstrap.NewRuntime(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	digest := r.Current("demo").SemanticManifest.Digest
	p := input.Projects["demo"]
	p.Documents[0].Content[0] = '!'
	delete(p.Config.SemanticSources, "sales")
	delete(input.Projects, "demo")
	if r.Current("demo").SemanticManifest.Digest != digest || len(r.Config.SemanticSources) != 1 || len(r.ProjectIDs()) != 1 {
		t.Fatal("caller mutation changed runtime")
	}
}

func TestMemoryRuntimeRejectsInvalidScopePlacementAndAuthority(t *testing.T) {
	for _, name := range []string{"missing-default", "missing-source", "invalid-document", "nil-authorizer", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			input := memoryInput(t)
			ctx := context.Background()
			var options []bootstrap.RuntimeOption
			switch name {
			case "missing-default":
				input.DefaultProject = "unknown"
			case "missing-source":
				p := input.Projects["demo"]
				p.DataSource = "unknown"
				input.Projects["demo"] = p
			case "invalid-document":
				input.Projects["demo"].Documents[0].Content = []byte("[invalid")
			case "nil-authorizer":
				options = append(options, bootstrap.WithProjectAuthorizer(nil))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			r, err := bootstrap.NewRuntime(ctx, input, options...)
			if err == nil {
				r.Close(context.Background())
				t.Fatal("invalid host input accepted")
			}
		})
	}
}
