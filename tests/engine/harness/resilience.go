package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
)

// ResilienceContract keeps fault expectations target-neutral. Backend test
// packages provide only the engine-local long-running statement required to
// exercise transport cancellation; no test seam is added to the Driver SPI.
type ResilienceContract struct {
	Name             string
	OpenExecution    func(*testing.T) *ProductionExecution
	LongRunningQuery string
}

// RunBackendResilienceContract proves the required failure and recovery
// behavior through the production Runner and Driver for one real engine.
func RunBackendResilienceContract(t *testing.T, contract ResilienceContract) {
	t.Helper()
	if contract.Name == "" || contract.OpenExecution == nil || strings.TrimSpace(contract.LongRunningQuery) == "" {
		t.Fatal("backend resilience contract is incomplete")
	}

	t.Run("cancel_before_execution", func(t *testing.T) {
		execution := contract.OpenExecution(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := execution.executeProbe(ctx, "SELECT 'ready'", ossie.DataTypeString, runner.ExecutionOptions{})
		assertResilienceFailure(t, result, err, runner.ExecutionCancelled)
		assertProbeHealthy(t, execution)
	})

	t.Run("row_limit_has_no_partial_result", func(t *testing.T) {
		execution := contract.OpenExecution(t)
		result, err := execution.executeProbe(context.Background(), "SELECT 'first' UNION ALL SELECT 'second'", ossie.DataTypeString, runner.ExecutionOptions{MaxRows: 1})
		assertResilienceFailure(t, result, err, runner.ExecutionLimit)
		assertProbeHealthy(t, execution)
	})

	t.Run("oversized_row_is_redacted", func(t *testing.T) {
		execution := contract.OpenExecution(t)
		const rowValue = "metis-row-value-must-not-leak"
		result, err := execution.executeProbe(context.Background(), "SELECT '"+rowValue+"'", ossie.DataTypeString, runner.ExecutionOptions{MaxBytes: 3})
		assertResilienceFailure(t, result, err, runner.ExecutionLimit)
		if strings.Contains(err.Error(), rowValue) {
			t.Fatalf("%s limit error leaked a row value: %v", contract.Name, err)
		}
		assertProbeHealthy(t, execution)
	})

	t.Run("query_timeout_recovers", func(t *testing.T) {
		execution := contract.OpenExecution(t)
		result, err := execution.executeProbe(context.Background(), contract.LongRunningQuery, ossie.DataTypeInteger, runner.ExecutionOptions{Timeout: 25 * time.Millisecond})
		assertResilienceFailure(t, result, err, runner.ExecutionTimeout)
		assertProbeHealthy(t, execution)
	})

	t.Run("close_rejects_new_execution", func(t *testing.T) {
		execution := contract.OpenExecution(t)
		execution.Close(t)
		result, err := execution.executeProbe(context.Background(), "SELECT 'ready'", ossie.DataTypeString, runner.ExecutionOptions{})
		assertResilienceFailure(t, result, err, runner.ExecutionClosed)
	})
}

func (e *ProductionExecution) executeProbe(ctx context.Context, statement string, datatype ossie.DataType, options runner.ExecutionOptions) (runner.ResultSet, error) {
	if e == nil || e.runtime == nil || e.route.Backend.Renderer == nil {
		return runner.ResultSet{}, errors.New("production conformance execution is not configured")
	}
	compiled, err := artifact.NewCompiledQuery(sql.SqlRenderResult{
		Dialect: e.route.Backend.SQLDialect(),
		SQL:     statement,
	}, artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "probe", Datatype: datatype}}})
	if err != nil {
		return runner.ResultSet{}, err
	}
	return e.runtime.ExecuteResolved(ctx, e.route, compiled, options)
}

func assertProbeHealthy(t *testing.T, execution *ProductionExecution) {
	t.Helper()
	result, err := execution.executeProbe(context.Background(), "SELECT 'ready'", ossie.DataTypeString, runner.ExecutionOptions{})
	if err != nil {
		t.Fatalf("healthy probe after fault: %v", err)
	}
	if result.Count != 1 || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0] != "ready" {
		t.Fatalf("healthy probe result = %#v", result)
	}
}

func assertResilienceFailure(t *testing.T, result runner.ResultSet, err error, want runner.ExecutionErrorCode) {
	t.Helper()
	var executionErr *runner.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != want {
		t.Fatalf("resilience error = %#v, want %s", err, want)
	}
	if result.Count != 0 || result.Bytes != 0 || len(result.Rows) != 0 {
		t.Fatalf("fault returned a partial result: %#v", result)
	}
}
