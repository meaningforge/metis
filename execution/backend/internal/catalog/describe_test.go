package catalog

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/execution/driver"
)

type descriptionDriver struct{}
type descriptionConn struct{ scenario string }
type descriptionRows struct {
	fields []string
	rows   [][]sqldriver.Value
	index  int
	fail   bool
}

func init() { sql.Register("metis-catalog-test", descriptionDriver{}) }
func (descriptionDriver) Open(scenario string) (sqldriver.Conn, error) {
	return &descriptionConn{scenario}, nil
}
func (*descriptionConn) Prepare(string) (sqldriver.Stmt, error) {
	return nil, errors.New("not supported")
}
func (*descriptionConn) Close() error                 { return nil }
func (*descriptionConn) Begin() (sqldriver.Tx, error) { return nil, errors.New("not supported") }
func (c *descriptionConn) QueryContext(ctx context.Context, query string, args []sqldriver.NamedValue) (sqldriver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 0 {
		return nil, errors.New("unexpected parameters")
	}
	if c.scenario == "permission" {
		return nil, errors.New("password=SECRET access denied")
	}
	clickhouse := strings.HasPrefix(query, "DESCRIBE TABLE")
	want := "DESCRIBE `Analytics`.`Orders`"
	if clickhouse {
		want = "DESCRIBE TABLE `Analytics`.`Orders` SETTINGS describe_include_subcolumns = 0"
	}
	if query != want {
		return nil, errors.New("wrong exact-object statement")
	}
	rows := &descriptionRows{fields: []string{"Field", "Type", "Null", "Key", "Default", "Extra"}, rows: [][]sqldriver.Value{{"Region", "varchar(64)", "YES", "false", "secret-default", ""}, {"Amount", "decimal(18,4)", "NO", "false", nil, ""}}}
	if clickhouse {
		rows.fields = []string{"name", "type", "default_type", "default_expression", "comment", "codec_expression", "ttl_expression"}
		rows.rows = [][]sqldriver.Value{{"Region", "LowCardinality(Nullable(String))", "", "secret-default", "secret-comment", "", ""}, {"Amount", "Decimal(18,4)", "", "", "", "", ""}}
	}
	switch c.scenario {
	case "empty":
		rows.rows = nil
	case "duplicate":
		rows.rows = append(rows.rows, rows.rows[0])
	case "layout":
		rows.fields[0] = "wrong"
	case "stream":
		rows.fail = true
	}
	return rows, nil
}
func (r *descriptionRows) Columns() []string { return r.fields }
func (*descriptionRows) Close() error        { return nil }
func (r *descriptionRows) Next(values []sqldriver.Value) error {
	if r.index == len(r.rows) {
		if r.fail {
			return errors.New("secret stream error")
		}
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}

func TestExactDescriptions(t *testing.T) {
	ref := driver.CatalogReference{ID: "orders", Parts: []string{"Analytics", "Orders"}}
	for _, backend := range []string{"doris", "clickhouse"} {
		db, err := sql.Open("metis-catalog-test", "")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		parse := ParseDoris
		if backend == "clickhouse" {
			parse = ParseClickHouse
		}
		result, err := Describe(context.Background(), db, backend, ref, driver.CatalogLimits{MaxColumns: 10, MaxBytes: 10000}, parse)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Reference, ref) || len(result.Columns) != 2 || !result.ColumnsComplete || result.Columns[0].Nullable != "nullable" || result.Columns[1].NativeType.Scale == nil || *result.Columns[1].NativeType.Scale != 4 {
			t.Fatalf("%#v", result)
		}
	}
}
func TestDescriptionsFailClosed(t *testing.T) {
	for _, scenario := range []string{"permission", "empty", "duplicate", "layout", "stream", "columns", "bytes", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := sql.Open("metis-catalog-test", scenario)
			defer db.Close()
			limits := driver.CatalogLimits{MaxColumns: 10, MaxBytes: 10000}
			if scenario == "columns" {
				limits.MaxColumns = 1
			}
			if scenario == "bytes" {
				limits.MaxBytes = 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			result, err := Describe(ctx, db, "doris", driver.CatalogReference{ID: "orders", Parts: []string{"Analytics", "Orders"}}, limits, ParseDoris)
			if err == nil || len(result.Columns) != 0 {
				t.Fatalf("partial evidence=%#v err=%v", result, err)
			}
		})
	}
}
