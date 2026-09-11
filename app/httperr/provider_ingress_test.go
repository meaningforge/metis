package httperr_test

import (
	"net/http"
	"testing"

	"github.com/meaningforge/metis/app/httperr"
)

func TestProviderIngressStatusProjection(t *testing.T) {
	for failure, expected := range map[httperr.ProviderIngressFailure]int{
		httperr.ProviderIngressRejected:        http.StatusUnauthorized,
		httperr.ProviderIngressPayloadTooLarge: http.StatusRequestEntityTooLarge,
		httperr.ProviderIngressUnavailable:     http.StatusServiceUnavailable,
		0:                                      http.StatusInternalServerError,
	} {
		if actual := httperr.ProviderIngressStatusOf(failure); actual != expected {
			t.Errorf("ProviderIngressStatusOf(%d) = %d, want %d", failure, actual, expected)
		}
	}
}
