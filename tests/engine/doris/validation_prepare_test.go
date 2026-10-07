package doris_test

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// The pinned engine only parses/stores PREPARE. This deliberately nonexistent
// relation must not become accepted online-planning evidence from PREPARE alone.
func TestPrepareDoesNotEstablishCatalogOrPlanningAcceptance(t *testing.T) {
	db, err := sql.Open("mysql", dorisDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	statement, err := db.PrepareContext(ctx, "SELECT absent_column FROM analytics.metis_absent_prepare_evidence WHERE absent_column = ?")
	if err != nil {
		t.Fatalf("pinned PREPARE behavior changed; review online validation capability: %v", err)
	}
	if err := statement.Close(); err != nil {
		t.Fatal(err)
	}
}
