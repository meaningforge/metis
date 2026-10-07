package catalog

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
	renderedsql "github.com/meaningforge/metis/renderer/sql"
)

type probeDriver struct{}
type probeConn struct{ scenario string }

func init()                                               { sql.Register("metis-probe-test", probeDriver{}) }
func (probeDriver) Open(s string) (sqldriver.Conn, error) { return &probeConn{s}, nil }
func (*probeConn) Prepare(string) (sqldriver.Stmt, error) {
	return nil, errors.New("unexpected preparation")
}
func (*probeConn) Close() error                 { return nil }
func (*probeConn) Begin() (sqldriver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *probeConn) QueryContext(ctx context.Context, text string, args []sqldriver.NamedValue) (sqldriver.Rows, error) {
	if c.scenario == "no_io" {
		panic("unsupported validation contacted database")
	}
	if text != "EXPLAIN SELECT 1" || len(args) != 0 {
		return nil, errors.New("changed original SQL or parameter")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	value := "plan"
	if c.scenario == "large" {
		value = strings.Repeat("x", 101)
	}
	return &descriptionRows{fields: []string{"plan"}, rows: [][]sqldriver.Value{{value}}}, nil
}

func TestExplainPreservesSQLAndBounds(t *testing.T) {
	compiled := &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{SQL: "SELECT 1"}}
	for _, scenario := range []string{"accepted", "large", "no_io"} {
		db, err := sql.Open("metis-probe-test", scenario)
		if err != nil {
			t.Fatal(err)
		}
		backend := "doris"
		if scenario == "no_io" {
			backend = "clickhouse"
			compiled = &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{SQL: "SELECT ?", Parameters: []renderedsql.QueryParameter{{Value: "private-parameter"}}}}
		}
		evidence, err := Explain(context.Background(), db, compiled, driver.CatalogLimits{MaxColumns: 10, MaxBytes: 100}, backend)
		db.Close()
		switch scenario {
		case "accepted":
			if err != nil || evidence.Outcome != "accepted" {
				t.Fatalf("evidence=%+v err=%v", evidence, err)
			}
		case "large":
			if err == nil {
				t.Fatal("accepted oversized planning evidence")
			}
		case "no_io":
			if err != nil || evidence.Outcome != "unsupported" {
				t.Fatalf("evidence=%+v err=%v", evidence, err)
			}
		}
	}
}

func TestDorisParameterizedExplainRemainsUnsupported(t *testing.T) {
	db, err := sql.Open("metis-probe-test", "no_io")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	compiled := &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{SQL: "SELECT ?", Parameters: []renderedsql.QueryParameter{{Value: "private-parameter"}}}}
	evidence, err := Explain(context.Background(), db, compiled, driver.CatalogLimits{MaxColumns: 10, MaxBytes: 100}, "doris")
	if err != nil || evidence.Outcome != "unsupported" {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}
