package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

// Authentication is rejected at the HTTP boundary before any service runs, so
// it is the failure most likely to drift into a private response shape. An
// Agent parsing errors must see the same payload here as everywhere else.
func TestUnauthenticatedResponseUsesTheSharedErrorPayload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	newProtectedRouter(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
	}

	var payload serrors.Payload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Code != string(serrors.ErrUnauthenticated) {
		t.Fatalf("code = %q, want %q", payload.Code, serrors.ErrUnauthenticated)
	}
	if payload.CallerAction != serrors.CallerActionAuthenticate {
		t.Fatalf("caller_action = %q, want %q", payload.CallerAction, serrors.CallerActionAuthenticate)
	}
	if payload.Message == "" {
		t.Fatal("message is empty")
	}
}
