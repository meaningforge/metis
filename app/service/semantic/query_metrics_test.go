package semantic

import (
	"testing"

	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
)

func TestQueryMetricsExecutionErrorProjectionIsBoundedAndRedacted(t *testing.T) {
	for _, test := range []struct {
		name       string
		code       runner.ExecutionErrorCode
		want       serrors.ErrorCode
		wantAction serrors.CallerAction
	}{
		{name: "invalid input", code: runner.ExecutionInvalidInput, want: serrors.ErrQueryExecutionFailed, wantAction: serrors.CallerActionChangeTarget},
		{name: "configuration", code: runner.ExecutionConfig, want: serrors.ErrQueryExecutionUnavailable, wantAction: serrors.CallerActionChangeTarget},
		{name: "capacity", code: runner.ExecutionCapacity, want: serrors.ErrQueryExecutionBusy, wantAction: serrors.CallerActionChangeRequest},
		{name: "closed", code: runner.ExecutionClosed, want: serrors.ErrQueryExecutionBusy, wantAction: serrors.CallerActionChangeRequest},
		{name: "secret", code: runner.ExecutionSecret, want: serrors.ErrQueryExecutionFailed, wantAction: serrors.CallerActionChangeTarget},
		{name: "open", code: runner.ExecutionOpen, want: serrors.ErrQueryExecutionFailed, wantAction: serrors.CallerActionChangeTarget},
		{name: "limit", code: runner.ExecutionLimit, want: serrors.ErrQueryExecutionLimit, wantAction: serrors.CallerActionChangeRequest},
		{name: "timeout", code: runner.ExecutionTimeout, want: serrors.ErrQueryExecutionTimeout, wantAction: serrors.CallerActionChangeRequest},
		{name: "cancelled", code: runner.ExecutionCancelled, want: serrors.ErrQueryExecutionCancelled, wantAction: serrors.CallerActionChangeRequest},
		{name: "driver", code: runner.ExecutionDriver, want: serrors.ErrQueryExecutionFailed, wantAction: serrors.CallerActionChangeTarget},
		{name: "cleanup", code: runner.ExecutionCleanup, want: serrors.ErrQueryExecutionFailed, wantAction: serrors.CallerActionChangeTarget},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := mapExecutionError(&runner.ExecutionError{Code: test.code, Message: "password=not-for-public-output"})
			semanticErr, ok := err.(*serrors.Error)
			if !ok || semanticErr.Code != test.want {
				t.Fatalf("mapped error = %#v, want %s", err, test.want)
			}
			if action := serrors.CallerActionOf(semanticErr.Code); action != test.wantAction {
				t.Fatalf("caller action = %s, want %s", action, test.wantAction)
			}
			if semanticErr.Message == "password=not-for-public-output" {
				t.Fatalf("public error leaked runtime message: %#v", semanticErr)
			}
		})
	}
}

func TestQueryMetricsResultContractProjectionIsStructuredAndValueFree(t *testing.T) {
	err := mapExecutionError(&runner.ExecutionError{
		Code:    runner.ExecutionResultContract,
		Message: "password=not-for-public-output",
		ResultContract: &runner.ResultContractFailure{
			Stage:            "result_normalization",
			Column:           "metric:sales.revenue",
			ExpectedDatatype: "Integer",
			ObservedFamily:   runner.ResultFamilyText,
		},
	})
	semanticErr, ok := err.(*serrors.Error)
	if !ok || semanticErr.Code != serrors.ErrQueryResultSchemaMismatch {
		t.Fatalf("mapped error = %#v", err)
	}
	if semanticErr.Details["stage"] != "result_normalization" || semanticErr.Details["column"] != "metric:sales.revenue" || semanticErr.Details["expected_datatype"] != "Integer" || semanticErr.Details["observed_family"] != runner.ResultFamilyText || semanticErr.Details["retryable"] != false {
		t.Fatalf("details = %#v", semanticErr.Details)
	}
	if action := serrors.CallerActionOf(semanticErr.Code); action != serrors.CallerActionChangeTarget {
		t.Fatalf("caller action = %s, want %s", action, serrors.CallerActionChangeTarget)
	}
	if semanticErr.Message == "password=not-for-public-output" {
		t.Fatalf("public error leaked runtime message: %#v", semanticErr)
	}
}
