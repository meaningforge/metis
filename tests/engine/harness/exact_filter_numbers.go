package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
)

func compileExactFilterNumbers(t *testing.T, selected renderer.Renderer) *artifact.CompiledQuery {
	t.Helper()
	const project = "exact-filter-reference"
	dimension := &ossie.Dimension{}
	doc := &ossie.Document{
		Version: ossie.SupportedSpecVersion,
		SemanticModel: []ossie.SemanticModel{{
			Name: "exact_filters",
			Datasets: []ossie.Dataset{{
				Name: "exact_filter_numbers", Source: "analytics.exact_filter_numbers",
				Fields: []ossie.Field{
					{Name: "id", Datatype: ossie.DataTypeString, Dimension: dimension, Expression: ansiExpression("exact_filter_numbers.id")},
					{Name: "large_id", Datatype: ossie.DataTypeInteger, Dimension: dimension, Expression: ansiExpression("exact_filter_numbers.large_id")},
					{Name: "exact_amount", Datatype: ossie.DataTypeDecimal, Dimension: dimension, Expression: ansiExpression("exact_filter_numbers.exact_amount")},
				},
			}},
		}},
	}
	snapshot, err := manifest.BuildProjectManifest(project, doc)
	if err != nil {
		t.Fatal(err)
	}
	var semanticQuery query.SemanticQuery
	body := `{"project":"exact-filter-reference","model":"exact_filters","dimensions":[{"name":"id"}],"filters":{"kind":"and","children":[{"kind":"filter","filter":{"field":"large_id","operator":"eq","value":"9007199254740993"}},{"kind":"filter","filter":{"field":"exact_amount","operator":"eq","value":"0.10000000000000000001"}}]}}`
	if err := json.Unmarshal([]byte(body), &semanticQuery); err != nil {
		t.Fatal(err)
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

func ansiExpression(text string) ossie.Expression {
	return ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: text}}}
}
