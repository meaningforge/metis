//go:build !duckdb

package command

import (
	"fmt"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
)

func newBundleDuckDBExecution(string, workload.Bundle) (readiness.Execution, func() error, error) {
	return nil, nil, fmt.Errorf("generated workloads require the DuckDB build; run make s2sbench-build")
}
