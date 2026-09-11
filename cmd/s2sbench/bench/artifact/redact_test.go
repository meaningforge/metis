package artifact

import (
	"strings"
	"testing"
)

func TestRedactStringRemovesEnvironmentAndStructuredSecrets(t *testing.T) {
	t.Setenv("S2SBENCH_TEST_API_KEY", "top-secret-value")
	input := `failed top-secret-value authorization: Bearer bearer-value {"access_token":"json-value"}`
	redacted := RedactString(input)
	for _, secret := range []string{"top-secret-value", "bearer-value", "json-value"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted text leaked %q: %s", secret, redacted)
		}
	}
}
