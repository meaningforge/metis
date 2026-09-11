package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeCreatesExecutableSelfContainedAdapter(t *testing.T) {
	driver, cleanup, err := Materialize()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	info, err := os.Stat(driver)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("driver mode = %v, want executable", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(driver), "metis.ts")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"S2SBENCH_PI_AUTH_FILE", "seedIsolatedPiAuth", "copyFile(source, destination)",
		"toolStartedAt", "Date.now() - startedAt",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("embedded Pi adapter omits %q", want)
		}
	}
}
