package doris_test

import (
	"database/sql"
	"testing"

	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestAuthoringWalkthroughThroughCLIAndREST(t *testing.T) {
	db, err := sql.Open("mysql", dorisDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	waitForBackend(t, db)
	for _, statement := range harness.AuthoringSetupStatements(t, "doris") {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("Doris walkthrough fixture setup failed: %v", err)
		}
	}
	// Provision a real authenticated reader in this disposable test database.
	// The CLI itself cannot create users, grant privileges or execute setup SQL.
	for _, statement := range []string{
		"CREATE USER 'metis_walkthrough'@'%' IDENTIFIED BY 'walkthrough_test_password'",
		"GRANT SELECT_PRIV ON metis_authoring_demo.* TO 'metis_walkthrough'@'%'",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("provision walkthrough test reader: %v", err)
		}
	}
	harness.RunAuthoringWalkthrough(t, "doris", "metis_walkthrough", "walkthrough_test_password")
}
