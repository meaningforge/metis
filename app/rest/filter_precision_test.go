package rest

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	service "github.com/meaningforge/metis/app/service/semantic"
)

func TestRESTFilterOperandsUseStringLiteralsBeforeServices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterCompileRoutes(r.Group("/v1"), service.NewCompileService(nil, nil, nil))
	RegisterQueryMetricsRoutes(r.Group("/v1"), nil)
	for _, path := range []string{"/compile-sql", "/explain", "/validate", "/query-metrics"} {
		for _, operand := range []string{`"9007199254740993"`, `"0.10000000000000000001"`, `["9007199254740992","9007199254740993"]`, `["0","1e-400"]`} {
			body := `{"query":{"project":"demo","model":"sales","metrics":[{"name":"revenue"}],"filters":{"kind":"filter","filter":{"field":"amount","operator":"in","value":` + operand + `}}}}`
			req := httptest.NewRequest("POST", "/v1"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code == 400 || strings.Contains(w.Body.String(), "INVALID_QUERY") {
				t.Fatalf("%s status%d: %s", path, w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), operand) {
				t.Fatal("response disclosed operand")
			}
		}
	}
	for _, path := range []string{"/compile-sql", "/explain", "/validate", "/query-metrics"} {
		for _, value := range []string{"1", "true", `["1",2]`} {
			body := `{"query":{"filters":{"kind":"filter","filter":{"field":"amount","operator":"eq","value":` + value + `}}}}`
			req := httptest.NewRequest("POST", "/v1"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "INVALID_QUERY") {
				t.Fatalf("%s non-string operand status%d: %s", path, w.Code, w.Body.String())
			}
		}
	}
}
