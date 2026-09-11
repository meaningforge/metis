package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/auth"
)

func TestMetricsRouteIsAbsentWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerMetricsRoute(router, nil)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled metrics status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestMetricsRouteIsOperatorSurfaceOutsideBearerAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerMetricsRoute(router, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("operator_metric 1\n"))
	}))

	verifier, err := auth.NewStaticAPIKeyVerifier("secret")
	if err != nil {
		t.Fatal(err)
	}
	protected := router.Group("/")
	protected.Use(auth.GinBearerMiddleware(verifier))
	protected.GET("/v1/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	metricsRecorder := httptest.NewRecorder()
	router.ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("operator metrics status = %d, want %d", metricsRecorder.Code, http.StatusOK)
	}

	protectedRecorder := httptest.NewRecorder()
	router.ServeHTTP(protectedRecorder, httptest.NewRequest(http.MethodGet, "/v1/protected", nil))
	if protectedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("protected route status = %d, want %d", protectedRecorder.Code, http.StatusUnauthorized)
	}
}
