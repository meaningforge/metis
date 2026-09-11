package doris_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	dorisbackend "github.com/meaningforge/metis/execution/backend/doris"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	testdatasource "github.com/meaningforge/metis/tests/engine/datasource"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestSharedSemanticScenarios(t *testing.T) {
	dsn := dorisDSN(t)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("connect to Doris: %v", err)
	}
	waitForBackend(t, db)
	harness.RunSharedExecutionContract(t, harness.Backend{
		Name: "doris",
		Prepare: func(t *testing.T, fixture fixtures.ID) {
			prepareFixture(t, db, fixture)
		},
		OpenExecution: func(t *testing.T) *harness.ProductionExecution {
			return openProductionExecution(t)
		},
	})
}

func TestExecutionBackendResilience(t *testing.T) {
	harness.RunBackendResilienceContract(t, harness.ResilienceContract{
		Name:             "doris",
		OpenExecution:    openProductionExecution,
		LongRunningQuery: "SELECT CAST(SLEEP(1) AS SIGNED)",
	})
}

func openProductionExecution(t *testing.T) *harness.ProductionExecution {
	t.Helper()
	testSource := testdatasource.Require(t, "doris")
	return harness.NewProductionExecution(t, testSource.RuntimeDataSource(harness.ProductionExecutionPolicy()),
		dorisbackend.New(), runner.NewEnvSecretResolver())
}

func dorisDSN(t *testing.T) string {
	t.Helper()
	dataSource := testdatasource.Require(t, "doris")
	if dataSource.Type() != "doris" {
		t.Fatalf("test DataSource type = %q, want doris", dataSource.Type())
	}
	config := mysql.NewConfig()
	config.Net = "tcp"
	config.Addr = net.JoinHostPort(dataSource.RequireValue(t, "host"), dataSource.RequireValue(t, "port"))
	config.User = dataSource.RequireValue(t, "username")
	database := dataSource.RequireValue(t, "database")
	if password, ok := dataSource.Secret("password"); ok {
		config.Passwd = password
	}
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 60 * time.Second
	config.WriteTimeout = 60 * time.Second
	bootstrap, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("open Doris bootstrap connection: %v", err)
	}
	if _, err := bootstrap.Exec("CREATE DATABASE IF NOT EXISTS `" + strings.ReplaceAll(database, "`", "``") + "`"); err != nil {
		_ = bootstrap.Close()
		t.Fatalf("create Doris test database: %v", err)
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatalf("close Doris bootstrap connection: %v", err)
	}
	config.DBName = database
	return config.FormatDSN()
}

func waitForBackend(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		var available int
		lastErr = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM backends() WHERE `alive` = true").Scan(&available)
		if lastErr == nil && available > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Doris backend did not become available: %v", lastErr)
		case <-ticker.C:
		}
	}
}

func prepareFixture(t *testing.T, db *sql.DB, id fixtures.ID) {
	t.Helper()
	statements, err := enginefixture.Statements(id, dorisFixtureDialect{})
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, "CREATE DATABASE IF NOT EXISTS analytics")
	for _, statement := range statements {
		execSQL(t, db, statement)
	}
}

type dorisFixtureDialect struct{}

func (dorisFixtureDialect) CreateTable(table enginefixture.Table) (string, error) {
	if len(table.KeyColumns) == 0 {
		return "", fmt.Errorf("Doris fixture table %q requires key columns", table.Name)
	}
	columns := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		physicalType, err := dorisFixtureType(column.Type)
		if err != nil {
			return "", err
		}
		if column.Nullable {
			physicalType += " NULL"
		}
		columns[i] = column.Name + " " + physicalType
	}
	keys := strings.Join(table.KeyColumns, ", ")
	return fmt.Sprintf(
		"CREATE TABLE analytics.%s (%s) DUPLICATE KEY(%s) DISTRIBUTED BY HASH(%s) BUCKETS 1 PROPERTIES (\"replication_num\" = \"1\")",
		table.Name, strings.Join(columns, ", "), keys, table.KeyColumns[0],
	), nil
}

func dorisFixtureType(logicalType enginefixture.LogicalType) (string, error) {
	switch logicalType {
	case enginefixture.String:
		return "VARCHAR(64)", nil
	case enginefixture.Integer:
		return "BIGINT", nil
	case enginefixture.Float:
		return "DOUBLE", nil
	case enginefixture.Decimal:
		return "DECIMAL(20,12)", nil
	case enginefixture.Boolean:
		return "BOOLEAN", nil
	case enginefixture.Date:
		return "DATE", nil
	case enginefixture.DateTime:
		return "DATETIME", nil
	default:
		return "", fmt.Errorf("unsupported Doris fixture type %q", logicalType)
	}
}

func execSQL(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatalf("execute Doris fixture statement: %v\nSQL: %s", err, statement)
	}
}

// recreateTable remains available for focused engine-specific fixtures whose
// schemas do not belong to the canonical semantic scenario corpus.
func recreateTable(t *testing.T, db *sql.DB, name, columns, key string) {
	t.Helper()
	execSQL(t, db, "DROP TABLE IF EXISTS analytics."+name)
	execSQL(t, db, fmt.Sprintf(
		"CREATE TABLE analytics.%s (%s) DUPLICATE KEY(%s) DISTRIBUTED BY HASH(%s) BUCKETS 1 PROPERTIES (\"replication_num\" = \"1\")",
		name, columns, key, strings.Split(key, ",")[0],
	))
}
