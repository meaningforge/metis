package rest

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRESTRejectsLossyFiltersBeforeServices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterCompileRoutes(r.Group("/v1"), nil)
	RegisterQueryMetricsRoutes(r.Group("/v1"), nil)
	for _, path := range []string{"/compile-sql", "/explain", "/validate", "/query-metrics"} {
		for _, operand := range []string{"9007199254740993", "0.10000000000000000001", "[9007199254740992,9007199254740993]", "[0,1e-400]"} {
			body := `{"query":{"filters":{"kind":"filter","filter":{"field":"amount","operator":"in","value":` + operand + `}}}}`
			req := httptest.NewRequest("POST", "/v1"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "INVALID_QUERY") {
				t.Fatalf("%s status%d: %s", path, w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), operand) {
				t.Fatal("response disclosed operand")
			}
		}
	}
}
