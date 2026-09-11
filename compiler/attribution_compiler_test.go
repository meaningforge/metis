package compiler_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

type attributionTestRenderer struct {
	dimensions  []string
	renderCalls int
	failAt      int
}

func (*attributionTestRenderer) SQLDialect() sql.SQLDialect { return "DORIS" }
func (*attributionTestRenderer) ExpressionDialect() string  { return "DORIS" }
func (*attributionTestRenderer) Capabilities() renderer.Capabilities {
	return renderer.Capabilities{}
}
func (r *attributionTestRenderer) Render(plan *sqlplan.Plan) (sql.SQLRenderResult, error) {
	r.renderCalls++
	dimension := ""
	for _, block := range plan.Blocks {
		if block.ID == plan.Root && len(block.Projections) > 0 {
			dimension = block.Projections[0].Alias
			break
		}
	}
	r.dimensions = append(r.dimensions, dimension)
	if r.failAt > 0 && r.renderCalls == r.failAt {
		return sql.SQLRenderResult{}, fmt.Errorf("forced render failure")
	}
	return sql.SQLRenderResult{Dialect: r.SQLDialect(), SQL: "SELECT 1"}, nil
}

type countingRendererResolver struct {
	renderer renderer.Renderer
	calls    int
}

func (r *countingRendererResolver) Resolve(sql.SQLDialect) (renderer.Renderer, error) {
	r.calls++
	return r.renderer, nil
}

func TestCompilerCompilesAttributionBundleInCanonicalOrderWithOneRendererSelection(t *testing.T) {
	selected := &attributionTestRenderer{}
	resolver := &countingRendererResolver{renderer: selected}
	physicalCompiler := compiler.NewCompiler(resolver)
	bundle := attributionBundleForCompilerTest(t, "country", "channel")

	got, err := physicalCompiler.CompileMetricAttributionBundle(context.Background(), bundle, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 {
		t.Fatalf("Renderer resolutions = %d, want 1", resolver.calls)
	}
	if !reflect.DeepEqual(selected.dimensions, []string{"channel", "country"}) {
		t.Fatalf("compile order = %#v", selected.dimensions)
	}
	if len(got.Queries) != 2 || got.Queries[0].Dimension != "channel" || got.Queries[1].Dimension != "country" {
		t.Fatalf("compiled bundle = %#v", got.Queries)
	}
	for _, query := range got.Queries {
		if len(query.OutputSchema.Columns) != 6 || query.OutputSchema.Columns[0].Name != query.Dimension {
			t.Fatalf("dimension %q output schema = %#v", query.Dimension, query.OutputSchema)
		}
	}
}

func TestCompilerRejectsNilCompilerForAttributionBundle(t *testing.T) {
	bundle := attributionBundleForCompilerTest(t, "country")
	selected := &attributionTestRenderer{}

	var nilCompiler *compiler.Compiler
	_, err := nilCompiler.CompileMetricAttributionBundleResolved(context.Background(), bundle, selected)
	assertCompilerErrorCode(t, err, serrors.ErrInternalInvariant)

	unconfigured := compiler.NewCompiler(nil)
	_, err = unconfigured.CompileMetricAttributionBundle(context.Background(), bundle, "DORIS")
	assertCompilerErrorCode(t, err, serrors.ErrInternalInvariant)
}

func TestCompilerRejectsNilAttributionBundle(t *testing.T) {
	selected := &attributionTestRenderer{}
	physicalCompiler := compiler.NewCompiler(&countingRendererResolver{renderer: selected})

	_, err := physicalCompiler.CompileMetricAttributionBundle(context.Background(), nil, "DORIS")
	assertCompilerErrorCode(t, err, serrors.ErrInternalInvariant)
}

func TestCompilerRejectsNilRendererForAttributionBundle(t *testing.T) {
	physicalCompiler := compiler.NewCompiler(&countingRendererResolver{})
	bundle := attributionBundleForCompilerTest(t, "country")

	_, err := physicalCompiler.CompileMetricAttributionBundleResolved(context.Background(), bundle, nil)
	assertCompilerErrorCode(t, err, serrors.ErrTargetRequired)
}

func TestCompilerHonorsCancelledContextBeforeAttributionCompilation(t *testing.T) {
	selected := &attributionTestRenderer{}
	physicalCompiler := compiler.NewCompiler(&countingRendererResolver{renderer: selected})
	bundle := attributionBundleForCompilerTest(t, "country")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := physicalCompiler.CompileMetricAttributionBundleResolved(ctx, bundle, selected)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if selected.renderCalls != 0 {
		t.Fatalf("render calls = %d, want 0", selected.renderCalls)
	}
}

func TestCompilerStopsAtFirstAttributionQueryCompilationFailure(t *testing.T) {
	selected := &attributionTestRenderer{failAt: 2}
	resolver := &countingRendererResolver{renderer: selected}
	physicalCompiler := compiler.NewCompiler(resolver)
	bundle := attributionBundleForCompilerTest(t, "region", "channel", "country")

	got, err := physicalCompiler.CompileMetricAttributionBundle(context.Background(), bundle, "DORIS")
	if err == nil {
		t.Fatal("expected compilation failure")
	}
	if got != nil {
		t.Fatalf("compiled bundle = %#v, want nil", got)
	}
	if resolver.calls != 1 {
		t.Fatalf("Renderer resolutions = %d, want 1", resolver.calls)
	}
	if selected.renderCalls != 2 {
		t.Fatalf("render calls = %d, want 2", selected.renderCalls)
	}
	if !strings.Contains(err.Error(), `dimension "country"`) {
		t.Fatalf("error = %v, want failing dimension", err)
	}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Details["cause"] != "forced render failure" {
		t.Fatalf("error = %#v, want structured Renderer cause", err)
	}
}

func attributionBundleForCompilerTest(t *testing.T, dimensions ...string) *attribution.MetricAttributionBundle {
	t.Helper()
	request := attributionRequestForPipelineTest()
	request.Dimensions = append([]string(nil), dimensions...)
	queries := make([]attribution.MetricAttributionDimensionPlan, 0, len(dimensions))
	for _, dimension := range dimensions {
		queries = append(queries, attribution.MetricAttributionDimensionPlan{
			Dimension: dimension,
			Plan:      attributionPlanForPipelineTest(t, request, dimension),
		})
	}
	bundle, err := attribution.BuildMetricAttributionBundle(request, queries)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func assertCompilerErrorCode(t *testing.T, err error, want serrors.ErrorCode) {
	t.Helper()
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) {
		t.Fatalf("error = %v, want *serrors.Error", err)
	}
	if metisErr.Code != want {
		t.Fatalf("error code = %q, want %q", metisErr.Code, want)
	}
}
