package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/auth"
)

func newProtectedRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	verifier, err := auth.NewStaticAPIKeyVerifier("secret")
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	protected := router.Group("")
	protected.Use(auth.GinBearerMiddleware(verifier))
	protected.Any("/protected", func(c *gin.Context) {
		principal, ok := auth.PrincipalFromContext(c.Request.Context())
		if !ok {
			t.Fatal("principal missing from request context")
		}
		if principal.TenantID != "local" || principal.SubjectID != "local" || principal.APIKeyID != "env" {
			t.Fatalf("unexpected principal: %#v", principal)
		}
		c.Status(http.StatusNoContent)
	})
	return router
}

func TestGinBearerMiddlewareAuthenticatesAndInjectsPrincipal(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	newProtectedRouter(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGinBearerMiddlewareRejectsMissingOrInvalidToken(t *testing.T) {
	for _, header := range []string{"", "Bearer wrong", "Basic secret"} {
		req := httptest.NewRequest(http.MethodPost, "/protected", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		newProtectedRouter(t).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header=%q status=%d", header, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("missing bearer challenge for header=%q", header)
		}
	}
}
