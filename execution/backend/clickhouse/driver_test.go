package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
)

type clickHouseConfigSecrets map[datasource.SecretRef]string

func (s clickHouseConfigSecrets) Value(reference datasource.SecretRef) (string, bool) {
	value, ok := s[reference]
	return value, ok
}

func TestClickHouseRuntimeRejectsCancelledAcquisitionAndAcquisitionAfterClose(t *testing.T) {
	db, err := sql.Open("clickhouse", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime := newDataSourceRuntime(db)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Acquire(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Acquire(context.Background()); err == nil {
		t.Fatal("closed ClickHouse Runtime lent a new Executor")
	}
}

func TestNewBindsOneExecutableClickHouseBackend(t *testing.T) {
	binding := New()
	if binding.Type != "clickhouse" || binding.Renderer == nil || binding.DriverFactory == nil || binding.SQLDialect() != "CLICKHOUSE" {
		t.Fatalf("ClickHouse Backend = %#v", binding)
	}
	if _, err := backend.NewBackendRegistry(binding); err != nil {
		t.Fatalf("NewBackendRegistry(ClickHouse) error = %v", err)
	}
}

func TestClickHouseOptionsResolveReferencesWithoutMutatingConfig(t *testing.T) {
	config := map[string]string{
		"scheme": "http", "address": "${METIS_CLICKHOUSE_ADDRESS}",
		"username": "metis", "password": "${METIS_CLICKHOUSE_PASSWORD}", "database": "analytics",
	}
	secrets := clickHouseConfigSecrets{
		{Provider: "env", Key: "METIS_CLICKHOUSE_ADDRESS"}:  "clickhouse.internal:8123",
		{Provider: "env", Key: "METIS_CLICKHOUSE_PASSWORD"}: "secret",
	}
	options, err := clickHouseOptions(config, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Addr) != 1 || options.Addr[0] != "clickhouse.internal:8123" || options.Auth.Database != "analytics" || options.Auth.Username != "metis" || options.Auth.Password != "secret" {
		t.Fatalf("ClickHouse options = %#v", options)
	}
	if config["address"] != "${METIS_CLICKHOUSE_ADDRESS}" || config["password"] != "${METIS_CLICKHOUSE_PASSWORD}" {
		t.Fatalf("ClickHouse Config was mutated: %#v", config)
	}
}

func TestClickHouseDriverFactoryValidatesClosedConnectionConfig(t *testing.T) {
	factory := NewDriverFactory()
	valid := map[string]string{
		"scheme": "https", "address": "clickhouse.internal:8443", "database": "analytics",
		"username": "metis_reader", "password": "${METIS_CLICKHOUSE_PASSWORD}",
	}
	if err := factory.ValidateConfig(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, config := range map[string]map[string]string{
		"missing scheme":     {"address": "clickhouse.internal:8123"},
		"invalid scheme":     {"scheme": "tcp", "address": "clickhouse.internal:9000"},
		"referenced scheme":  {"scheme": "${METIS_SCHEME}", "address": "clickhouse.internal:8123"},
		"missing address":    {"scheme": "http"},
		"plaintext password": {"scheme": "http", "address": "clickhouse.internal:8123", "password": "secret"},
		"unknown field":      {"scheme": "http", "address": "clickhouse.internal:8123", "dialect": "CLICKHOUSE"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := factory.ValidateConfig(config); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
}
