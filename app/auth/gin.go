package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/httperr"
	"github.com/meaningforge/metis/serrors"
)

const GinPrincipalKey = "metis.principal"

// GinBearerMiddleware adapts the shared APIKeyVerifier to Gin REST routes.
func GinBearerMiddleware(verifier APIKeyVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		const prefix = "Bearer "
		if verifier == nil || !strings.HasPrefix(header, prefix) {
			abortUnauthorized(c)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		principal, err := verifier.Verify(c.Request.Context(), token)
		if err != nil {
			abortUnauthorized(c)
			return
		}
		c.Set(GinPrincipalKey, principal)
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

// abortUnauthorized emits the shared Metis error payload rather than a
// hand-built body, so an Agent parsing errors sees one response shape across
// every failure including authentication.
//
// The status comes from the same projection the REST handlers use. Writing 401
// here instead would define UNAUTHENTICATED -> 401 a second time, and the
// completeness test in app/httperr cannot see a literal in this package: the two
// would be free to drift the moment either changed.
func abortUnauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	err := &serrors.Error{Code: serrors.ErrUnauthenticated, Message: "valid bearer API key is required"}
	c.AbortWithStatusJSON(httperr.StatusOf(err.Code), serrors.PayloadFrom(err))
}
