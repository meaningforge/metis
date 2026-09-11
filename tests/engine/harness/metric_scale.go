package harness

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
)

// compileRegisteredMetricScale materializes the first semantic-critical
// extension through an explicitly registered selected-Renderer capability.
// The shared harness executes the complete artifact through production Runner,
// so every registered engine inherits the same semantic contract.
func compileRegisteredMetricScale(t *testing.T, selected renderer.Renderer) *artifact.CompiledQuery {
	t.Helper()

	const (
		project = "metric-scale-reference"
		model   = "metric_scale"
	)
	doc := &ossie.Document{
		Version: ossie.SupportedSpecVersion,
		SemanticModel: []ossie.SemanticModel{{
			Name: model,
			Datasets: []ossie.Dataset{{
				Name:   "metric_scale_orders",
				Source: "analytics.metric_scale_orders",
				Fields: []ossie.Field{{
					Name:     "amount",
					Datatype: ossie.DataTypeDecimal,
					Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{
						Dialect: ossie.DialectANSISQL, Expression: "metric_scale_orders.amount",
					}}},
				}},
			}},
			Metrics: []ossie.Metric{{
				Name:     "revenue",
				Datatype: ossie.DataTypeDecimal,
				Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{
					Dialect: ossie.DialectANSISQL, Expression: "SUM(metric_scale_orders.amount)",
				}}},
				CustomExtensions: []ossie.CustomExtension{{
					VendorName: extension.MetricScaleVendor,
					Data:       `{"kind":"metric_scale","version":"1","factor":2}`,
				}},
			}},
		}},
	}

	snapshot, err := manifest.BuildProjectManifest(project, doc)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil {
		t.Fatal("selected Renderer is required")
	}
	selectedRenderer := extension.RendererContext{Dialect: string(selected.SQLDialect())}
	inventory := extension.NewInventory()
	requirements, err := inventory.RequirementsForModel(&doc.SemanticModel[0])
	if err != nil {
		t.Fatal(err)
	}
	capabilities := extension.NewRegistry()
	if err := capabilities.Register(extension.MetricScaleRegistration(selectedRenderer)); err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 {
		t.Fatalf("metric scale requirements = %d, want 1", len(requirements))
	}
	if resolution := capabilities.Resolve(requirements[0], selectedRenderer); resolution.State != extension.StateSupported {
		t.Fatalf("metric scale capability resolution = %#v, want supported", resolution)
	}

	semanticQuery := query.SemanticQuery{
		Project: project,
		Model:   model,
		Metrics: []query.MetricRef{{Name: "revenue"}},
	}
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, selected)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, selected)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.CompileWithRenderer(context.Background(), plan, selected)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}
