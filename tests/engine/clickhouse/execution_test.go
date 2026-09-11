package clickhouse_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"testing"

	clickhousebackend "github.com/meaningforge/metis/execution/backend/clickhouse"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	testdatasource "github.com/meaningforge/metis/tests/engine/datasource"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestSharedSemanticScenarios(t *testing.T) {
	url := clickHouseURL(t)
	harness.RunSharedExecutionContract(t, harness.Backend{
		Name: "clickhouse",
		Prepare: func(t *testing.T, fixture fixtures.ID) {
			prepareFixture(t, url, fixture)
		},
		OpenExecution: func(t *testing.T) *harness.ProductionExecution {
			return openProductionExecution(t)
		},
	})
}

func TestExecutionBackendResilience(t *testing.T) {
	harness.RunBackendResilienceContract(t, harness.ResilienceContract{
		Name:             "clickhouse",
		OpenExecution:    openProductionExecution,
		LongRunningQuery: "SELECT CAST(sleep(1) AS Int64)",
	})
}

func openProductionExecution(t *testing.T) *harness.ProductionExecution {
	t.Helper()
	testSource := testdatasource.Require(t, "clickhouse")
	return harness.NewProductionExecution(t, testSource.RuntimeDataSource(harness.ProductionExecutionPolicy()),
		clickhousebackend.New(), runner.NewEnvSecretResolver())
}

func clickHouseURL(t *testing.T) string {
	t.Helper()
	dataSource := testdatasource.Require(t, "clickhouse")
	if dataSource.Type() != "clickhouse" {
		t.Fatalf("test DataSource type = %q, want clickhouse", dataSource.Type())
	}
	endpoint := &neturl.URL{
		Scheme: dataSource.RequireValue(t, "scheme"),
		Host:   dataSource.RequireValue(t, "address"),
		Path:   "/",
		User: neturl.UserPassword(
			dataSource.RequireValue(t, "username"),
			dataSource.RequireSecret(t, "password"),
		),
	}
	query := endpoint.Query()
	query.Set("database", dataSource.RequireValue(t, "database"))
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func prepareFixture(t *testing.T, url string, id fixtures.ID) {
	t.Helper()
	statements, err := enginefixture.Statements(id, clickHouseFixtureDialect{})
	if err != nil {
		t.Fatal(err)
	}
	statements = append([]string{"CREATE DATABASE IF NOT EXISTS analytics"}, statements...)
	prepareStatements(t, url, string(id), statements)
}

type clickHouseFixtureDialect struct{}

func (clickHouseFixtureDialect) CreateTable(table enginefixture.Table) (string, error) {
	columns := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		physicalType, err := clickHouseFixtureType(column.Type)
		if err != nil {
			return "", err
		}
		if column.Nullable {
			physicalType = "Nullable(" + physicalType + ")"
		}
		columns[i] = column.Name + " " + physicalType
	}
	return "CREATE TABLE analytics." + table.Name + " (" + strings.Join(columns, ", ") + ") ENGINE = Memory", nil
}

func clickHouseFixtureType(logicalType enginefixture.LogicalType) (string, error) {
	switch logicalType {
	case enginefixture.String:
		return "String", nil
	case enginefixture.Integer:
		return "Int64", nil
	case enginefixture.Float:
		return "Float64", nil
	case enginefixture.Decimal:
		// Canonical fixtures exercise a semantic Decimal contract over a
		// physically inexact source as well. The compiled root cast, not fixture
		// DDL, is responsible for delivering exact Decimal values to Runner.
		return "Float64", nil
	case enginefixture.Boolean:
		return "Bool", nil
	case enginefixture.Date:
		return "Date", nil
	case enginefixture.DateTime:
		return "DateTime", nil
	default:
		return "", fmt.Errorf("unsupported ClickHouse fixture type %q", logicalType)
	}
}

func prepareStatements(t *testing.T, url, name string, statements []string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := clickhouse(url, statement); err != nil {
			t.Fatalf("prepare ClickHouse %s fixture: %v\nSQL: %s", name, err, statement)
		}
	}
}

func clickhouse(url, sql string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(sql))
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ClickHouse HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}
