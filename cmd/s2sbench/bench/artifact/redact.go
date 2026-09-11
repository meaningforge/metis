// Package artifact owns cross-cutting persistence safety for S2SBench output.
package artifact

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

const Replacement = "[REDACTED]"

var (
	bearerPattern = regexp.MustCompile(`(?i)(authorization[[:space:]]*[:=][[:space:]]*bearer[[:space:]]+)[^[:space:]",}]+`)
	jsonSecret    = regexp.MustCompile(`(?i)("[^"\\]*(?:token|secret|password|api[_-]?key|authorization|credential)[^"\\]*"[[:space:]]*:[[:space:]]*")[^"]*(")`)
)

// RedactString removes known secret environment values and common structured
// authorization forms before text reaches logs, manifests, or diagnostics.
func RedactString(value string) string {
	secrets := make([]string, 0)
	for _, entry := range os.Environ() {
		name, secret, ok := strings.Cut(entry, "=")
		if ok && sensitiveName(name) && len(secret) >= 4 {
			secrets = append(secrets, secret)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, Replacement)
	}
	value = bearerPattern.ReplaceAllString(value, `${1}`+Replacement)
	return jsonSecret.ReplaceAllString(value, `${1}`+Replacement+`${2}`)
}

func sensitiveName(name string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "API_KEY", "APIKEY", "AUTHORIZATION", "CREDENTIAL"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
