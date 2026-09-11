package serrors_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestPayloadFromPreservesWrappedMetisError(t *testing.T) {
	err := fmt.Errorf("resolve metric: %w", &serrors.Error{
		Code:        serrors.ErrMetricNotFound,
		Message:     "metric not found",
		Details:     map[string]any{"metric": "revenue"},
		Suggestions: []string{"total_revenue"},
	})

	got := serrors.PayloadFrom(err)
	want := serrors.Payload{
		Code:         string(serrors.ErrMetricNotFound),
		CallerAction: serrors.CallerActionChangeRequest,
		Message:      "metric not found",
		Details:      map[string]any{"metric": "revenue"},
		Suggestions:  []string{"total_revenue"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestPayloadFromUsesInternalContractForUnknownError(t *testing.T) {
	got := serrors.PayloadFrom(errors.New("boom"))
	if got.Code != string(serrors.ErrInternal) || got.CallerAction != serrors.CallerActionReportDefect || got.Message != "boom" {
		t.Fatalf("payload = %#v", got)
	}
}
