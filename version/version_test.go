package version

import (
	"os"
	"testing"
)

func TestCurrentReportsInjectedBuildMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })
	Version, Commit, Date = "v-test", "deadbeef", "1970-01-01T00:00:00Z"

	got := Current()
	if got.Version != Version || got.Commit != Commit || got.Date != Date || got.Go == "" {
		t.Fatalf("Current() = %#v", got)
	}
}

func TestLegacyPkgDirectoryDoesNotReappear(t *testing.T) {
	if _, err := os.Stat("../pkg"); err == nil {
		t.Fatal("pkg exists; use a focused root package instead of a generic holding area")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect pkg: %v", err)
	}
}
