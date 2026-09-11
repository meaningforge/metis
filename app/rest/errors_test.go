package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/serrors"
)

func TestWriteJSONPreservesSharedErrorPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	writeJSON(ctx, nil, &serrors.Error{
		Code:        serrors.ErrMetricNotFound,
		Message:     "metric not found",
		Details:     map[string]any{"metric": "revenue"},
		Suggestions: []string{"total_revenue"},
	})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload serrors.Payload
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != string(serrors.ErrMetricNotFound) || len(payload.Suggestions) != 1 || payload.Suggestions[0] != "total_revenue" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.CallerAction != serrors.CallerActionChangeRequest {
		t.Fatalf("caller_action = %q", payload.CallerAction)
	}
}

func TestWriteJSONProjectsStatusFromTheCodeWithoutParsingNames(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		action serrors.CallerAction
	}{
		{name: "invalid request", err: &serrors.Error{Code: serrors.ErrInvalidQuery}, status: http.StatusBadRequest, action: serrors.CallerActionChangeRequest},
		{name: "not found", err: &serrors.Error{Code: serrors.ErrMetricNotFound}, status: http.StatusNotFound, action: serrors.CallerActionChangeRequest},
		{name: "conflict", err: &serrors.Error{Code: serrors.ErrAmbiguousTarget}, status: http.StatusConflict, action: serrors.CallerActionChangeTarget},
		{name: "unsupported", err: &serrors.Error{Code: serrors.ErrUnsupportedDialect}, status: http.StatusUnprocessableEntity, action: serrors.CallerActionChangeTarget},
		{name: "unauthenticated", err: &serrors.Error{Code: serrors.ErrUnauthenticated}, status: http.StatusUnauthorized, action: serrors.CallerActionAuthenticate},
		{name: "internal invariant violation", err: serrors.Internal("semantic plan invariant validation failed", nil), status: http.StatusInternalServerError, action: serrors.CallerActionReportDefect},
		{name: "query grain conflict stays client-actionable", err: &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain}, status: http.StatusBadRequest, action: serrors.CallerActionChangeRequest},
		// One status, two remediations: the class cannot tell these apart and
		// the action can. The error code must preserve that distinction.
		{name: "model defect shares the invalid-request status", err: &serrors.Error{Code: serrors.ErrInvalidMetricExtension}, status: http.StatusBadRequest, action: serrors.CallerActionChangeModel},
		{name: "unknown Go error", err: fmt.Errorf("boom"), status: http.StatusInternalServerError, action: serrors.CallerActionReportDefect},
		{name: "unregistered typed code", err: &serrors.Error{Code: "FUTURE_NOT_FOUND"}, status: http.StatusInternalServerError, action: serrors.CallerActionReportDefect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writeJSON(ctx, nil, tt.err)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.status, recorder.Body.String())
			}
			var payload serrors.Payload
			if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.CallerAction != tt.action {
				t.Fatalf("caller_action = %q, want %q", payload.CallerAction, tt.action)
			}
		})
	}
}

func TestWriteJSONPreservesUnsupportedExtensionDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	writeJSON(ctx, nil, &serrors.Error{
		Code:    serrors.ErrUnsupportedSemanticExtension,
		Message: "semantic-critical extension is unsupported by the selected compile target",
		Details: map[string]any{
			"extension_identity": "acme/fiscal_calendar/metric",
			"capability":         "fiscal_calendar",
			"version":            "1",
			"engine":             "metis-native",
			"dialect":            "doris",
			"reason":             "capability_missing",
		},
	})

	// 422: the request was understood and well-formed, but the selected target
	// cannot express the required extension. 400 would misreport that as a
	// malformed request.
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload serrors.Payload
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != string(serrors.ErrUnsupportedSemanticExtension) || payload.Details["reason"] != "capability_missing" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, leaked := payload.Details["data"]; leaked {
		t.Fatalf("raw extension payload leaked into REST response: %#v", payload.Details)
	}
}
