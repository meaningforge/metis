package extension

import "fmt"

// UnsupportedError is bounded, deterministic evidence that a semantic-critical
// extension cannot be satisfied by the selected compile path. It intentionally
// excludes the raw extension payload.
type UnsupportedError struct {
	Resolution Resolution
}

func (e *UnsupportedError) Error() string {
	if e == nil {
		return "unsupported semantic-critical extension"
	}
	resolution := e.Resolution
	return fmt.Sprintf(
		"unsupported semantic-critical extension: identity=%s capability=%s version=%s dialect=%s reason=%s",
		resolution.Identity.String(), resolution.Capability, resolution.Version,
		resolution.Renderer.Dialect, resolution.Reason,
	)
}
