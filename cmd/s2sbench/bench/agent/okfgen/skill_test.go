package okfgen

import (
	"os"
	"strings"
	"testing"
)

func TestMaterializeExtensionContainsGoogleReferenceAgentTools(t *testing.T) {
	extension, cleanup, err := MaterializeExtension()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	body, err := os.ReadFile(extension)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_concepts", "read_existing_doc", "read_concept_raw", "read_semantic_context", "sample_rows", "write_concept_doc"} {
		if !strings.Contains(string(body), name) {
			t.Errorf("extension omits %q", name)
		}
	}
}
