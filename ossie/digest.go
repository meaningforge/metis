package ossie

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Digest returns a deterministic digest of the schema-derived Ossie document.
// encoding/json sorts map keys, including maps nested inside AIContext, so the
// same semantic document produces a stable digest independent of YAML layout.
func Digest(doc *Document) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("nil ossie document")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal ossie document for digest: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
