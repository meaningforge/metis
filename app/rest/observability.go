package rest

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/httperr"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/serrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// TraceContextMiddleware extracts W3C context once at the authenticated HTTP
// boundary. REST and MCP children then share the same remote parent.
func TraceContextMiddleware(tracing *observability.Tracing) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := tracing.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// ObservationMiddleware records one REST operation using Gin's registered
// route template. Raw URL paths cannot pass Recorder validation.
func ObservationMiddleware(recorder *observability.Recorder, tracing *observability.Tracing) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		ctx, span := tracing.Start(c.Request.Context(), observability.OperationHTTP)
		c.Request = c.Request.WithContext(ctx)
		completed := false
		defer func() {
			status := c.Writer.Status()
			if !completed {
				status = httperr.StatusOf(serrors.ErrInternal)
				observability.RecordError(ctx, errors.New("HTTP handler terminated before completion"), serrors.ErrInternal)
			}
			route := observability.HTTPRoute(c.FullPath())
			method := observability.HTTPMethod(c.Request.Method)
			statusClass := observability.StatusClassFor(status)
			span.SetAttributes(
				attribute.String("http.request.method", string(method)),
				attribute.String("http.route", string(route)),
				attribute.Int("http.response.status_code", status),
			)
			span.End()
			recorder.Record(ctx, observability.HTTPObservation{
				Route: route, Method: method, StatusClass: statusClass, Duration: time.Since(started),
			})
		}()
		c.Next()
		completed = true
	}
}
