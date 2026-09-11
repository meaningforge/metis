package command

import (
	"fmt"
	"sync"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/compiler/artifact"
)

// physicalQueryCapture is benchmark-only evidence storage. Production
// query_metrics remains value-focused and does not expose physical SQL.
type physicalQueryCapture struct {
	mu      sync.Mutex
	queries map[string]*artifact.CompiledQuery
}

func newPhysicalQueryCapture() *physicalQueryCapture {
	return &physicalQueryCapture{queries: make(map[string]*artifact.CompiledQuery)}
}

func (c *physicalQueryCapture) record(queryID string, compiled *artifact.CompiledQuery) error {
	if c == nil || queryID == "" {
		return nil
	}
	snapshot, err := artifact.SnapshotCompiledQuery(compiled)
	if err != nil {
		return fmt.Errorf("snapshot benchmark executed query: %w", err)
	}
	c.mu.Lock()
	c.queries[queryID] = snapshot
	c.mu.Unlock()
	return nil
}

func (c *physicalQueryCapture) decorateAttempt(record readiness.AttemptRecord) (readiness.AttemptRecord, error) {
	if c == nil || record.QueryEvidence == nil || record.QueryEvidence.QueryID == "" {
		return record, nil
	}
	c.mu.Lock()
	compiled := c.queries[record.QueryEvidence.QueryID]
	delete(c.queries, record.QueryEvidence.QueryID)
	c.mu.Unlock()
	if compiled == nil {
		return record, fmt.Errorf("query_metrics %q has no captured executed query", record.QueryEvidence.QueryID)
	}
	snapshot, err := artifact.SnapshotCompiledQuery(compiled)
	if err != nil {
		return record, fmt.Errorf("snapshot captured executed query: %w", err)
	}
	record.ExecutedQuery = snapshot
	return record, nil
}
