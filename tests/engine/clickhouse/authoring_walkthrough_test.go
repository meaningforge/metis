package clickhouse_test

import (
	"testing"

	testdatasource "github.com/meaningforge/metis/tests/engine/datasource"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestAuthoringWalkthroughThroughCLIAndREST(t *testing.T) {
	source := testdatasource.Require(t, "clickhouse")
	prepareStatements(t, clickHouseURL(t), "authoring walkthrough", harness.AuthoringSetupStatements(t, "clickhouse"))
	harness.RunAuthoringWalkthrough(t, "clickhouse", source.RequireValue(t, "username"), source.RequireSecret(t, "password"))
}
