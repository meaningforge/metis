package httperr

import "net/http"

// ProviderIngressFailure is the closed transport-only failure vocabulary for
// externally signed provider callbacks. These endpoints deliberately return an
// empty body, so there is no serrors payload to project through StatusOf.
type ProviderIngressFailure uint8

const (
	ProviderIngressRejected ProviderIngressFailure = iota + 1
	ProviderIngressPayloadTooLarge
	ProviderIngressUnavailable
)

// ProviderIngressStatusOf centralizes empty provider-callback responses beside
// the ordinary Metis error projection. Unknown values fail closed to 500.
func ProviderIngressStatusOf(failure ProviderIngressFailure) int {
	switch failure {
	case ProviderIngressRejected:
		return http.StatusUnauthorized
	case ProviderIngressPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case ProviderIngressUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
