//go:build duckdb

package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/meaningforge/metis/execution/driver"
)

// dataSourceRuntime owns one process-scoped embedded DuckDB handle. Each
// Acquire returns a lightweight request lease; closing that lease never closes
// the shared database.
type dataSourceRuntime struct {
	db        *sql.DB
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func (r *dataSourceRuntime) Acquire(ctx context.Context) (driver.Executor, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("DuckDB DataSource runtime is not initialized")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("DuckDB DataSource runtime is closed")
	}
	return &executor{db: r.db}, nil
}

func (r *dataSourceRuntime) Close(ctx context.Context) error {
	if r == nil || r.db == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		r.closeDone = make(chan struct{})
		go func() {
			r.closeErr = r.db.Close()
			close(r.closeDone)
		}()
	})
	select {
	case <-r.closeDone:
		return r.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
