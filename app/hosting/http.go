// Package hosting assembles the public Core HTTP surface for standalone and
// embedded hosts. It contains no listener lifecycle, enterprise identity
// provider, GitOps, Release service or tenant-management routes.
package hosting

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/mcp"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/app/rest"
)

// HTTPOptions supplies host-owned HTTP dependencies. Verifier is mandatory;
// policy decisions are injected when constructing the bootstrap.Runtime.
// Metrics, when supplied, is an intentionally public operator endpoint.
type HTTPOptions struct {
	Verifier auth.APIKeyVerifier
	Recorder *observability.Recorder
	Tracing  *observability.Tracing
	Metrics  http.Handler
}

// NewHTTPHandler mounts identical authenticated REST/MCP services for the
// standalone binary and embedding hosts. Hosts own TLS, listening and shutdown.
func NewHTTPHandler(runtime *bootstrap.Runtime, options HTTPOptions) (http.Handler, error) {
	if runtime == nil || runtime.Generations == nil {
		return nil, fmt.Errorf("initialized Core runtime is required")
	}
	if options.Verifier == nil {
		return nil, fmt.Errorf("HTTP token verifier is required")
	}
	tracing := options.Tracing
	if tracing == nil {
		tracing = observability.NewTracing(nil)
	}
	recorder := options.Recorder
	if recorder == nil {
		recorder = observability.NewRecorder()
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", func(c *gin.Context) {
		projects := runtime.ProjectIDs()
		response := gin.H{"status": "ready", "projects": projects}
		if len(projects) == 1 {
			response["project"] = projects[0]
		}
		c.JSON(http.StatusOK, response)
	})
	if options.Metrics != nil {
		router.GET("/metrics", gin.WrapH(options.Metrics))
	}
	protected := router.Group("/")
	protected.Use(auth.GinBearerMiddleware(options.Verifier))
	protected.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(runtime.Generations.Pin(c.Request.Context()))
		c.Next()
	})
	protected.Use(rest.TraceContextMiddleware(tracing))
	v1 := protected.Group("/v1")
	v1.Use(rest.ObservationMiddleware(recorder, tracing))
	rest.RegisterDiscoveryRoutes(v1, runtime.Discovery)
	rest.RegisterCompileRoutes(v1, runtime.Compile)
	rest.RegisterQueryMetricsRoutes(v1, runtime.QueryMetrics)
	rest.RegisterDimensionValuesRoutes(v1, runtime.DimensionValues)
	rest.RegisterAttributeMetricRoutes(v1, runtime.AttributeMetric)
	rest.RegisterCompareMetricsRoutes(v1, runtime.CompareMetrics)
	protected.Any("/mcp", gin.WrapH(mcp.NewObservedHTTPHandlerWithDimensionValues(runtime.Discovery, runtime.Compile, runtime.QueryMetrics, runtime.DimensionValues, runtime.AttributeMetric, runtime.CompareMetrics, recorder, tracing, runtime.Generations)))
	return router, nil
}
