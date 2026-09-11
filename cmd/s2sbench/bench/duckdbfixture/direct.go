//go:build duckdb

package fixture

import (
	"context"
	"database/sql"

	"github.com/duckdb/duckdb-go/v2"
)

func (b *Backend) openDirect(ctx context.Context, readOnly bool) (*sql.DB, error) {
	dsn := b.Database
	if readOnly {
		dsn += "?access_mode=read_only"
	}
	connector, err := duckdb.NewConnector(dsn, nil)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
