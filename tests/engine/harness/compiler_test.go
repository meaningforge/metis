package harness

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/ossie"
	duckdbrenderer "github.com/meaningforge/metis/renderer/duckdb"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestCompileScenarioForRendererPreservesDecimalOutputContract(t *testing.T) {
	scenario, ok := scenarios.ByName("ratio_metric")
	if !ok {
		t.Fatal("ratio_metric scenario is not registered")
	}
	selected := duckdbrenderer.New()
	compiled := CompileScenarioForRenderer(t, scenario, selected)
	if compiled.PhysicalQuery.Dialect != selected.SQLDialect() {
		t.Fatalf("compiled dialect = %q, want %q", compiled.PhysicalQuery.Dialect, selected.SQLDialect())
	}
	if len(compiled.OutputSchema.Columns) != 1 || compiled.OutputSchema.Columns[0].Datatype != ossie.DataTypeDecimal {
		t.Fatalf("output schema = %#v", compiled.OutputSchema)
	}
	if !strings.Contains(compiled.PhysicalQuery.SQL, "AS DECIMAL(38,18))") ||
		strings.Contains(compiled.PhysicalQuery.SQL, "AS DECIMAL(20,12)) AS DECIMAL(38,18))") {
		t.Fatalf("Decimal output boundary is missing:\n%s", compiled.PhysicalQuery.SQL)
	}
}
