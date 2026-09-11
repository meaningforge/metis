package doris

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
)

type dorisConfigSecrets map[datasource.SecretRef]string

func (s dorisConfigSecrets) Value(reference datasource.SecretRef) (string, bool) {
	value, ok := s[reference]
	return value, ok
}

func TestDorisRuntimeRejectsCancelledAcquisitionAndAcquisitionAfterClose(t *testing.T) {
	db, err := sql.Open("mysql", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime := &dataSourceRuntime{db: db}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Acquire(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Acquire(context.Background()); err == nil {
		t.Fatal("closed Doris Runtime lent a new Executor")
	}
}

func TestNewBindsOneExecutableDorisBackend(t *testing.T) {
	binding := New()
	if binding.Type != "doris" || binding.Renderer == nil || binding.DriverFactory == nil || binding.SQLDialect() != "DORIS" {
		t.Fatalf("Doris Backend = %#v", binding)
	}
	if _, err := backend.NewBackendRegistry(binding); err != nil {
		t.Fatalf("NewBackendRegistry(Doris) error = %v", err)
	}
}

func TestDorisConnectionAddressResolvesReferencesWithoutMutatingConfig(t *testing.T) {
	config := map[string]string{"host": "${METIS_DORIS_HOST}", "port": "${METIS_DORIS_PORT}"}
	secrets := dorisConfigSecrets{
		{Provider: "env", Key: "METIS_DORIS_HOST"}: "doris.internal",
		{Provider: "env", Key: "METIS_DORIS_PORT"}: "9030",
	}
	address, err := connectionAddress(config, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if address != "doris.internal:9030" {
		t.Fatalf("Doris address = %q", address)
	}
	if config["host"] != "${METIS_DORIS_HOST}" || config["port"] != "${METIS_DORIS_PORT}" {
		t.Fatalf("Doris Config was mutated: %#v", config)
	}
}

func TestDorisDriverFactoryValidatesClosedConnectionConfig(t *testing.T) {
	factory := NewDriverFactory()
	valid := map[string]string{
		"host":     "doris.internal",
		"port":     "9030",
		"database": "analytics",
		"username": "metis_reader",
		"password": "${METIS_DORIS_PASSWORD}",
	}
	if err := factory.ValidateConfig(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	referencedConnection := map[string]string{
		"host": "${METIS_DORIS_HOST}",
		"port": "${METIS_DORIS_PORT}",
	}
	if err := factory.ValidateConfig(referencedConnection); err != nil {
		t.Fatalf("referenced connection config: %v", err)
	}
	for name, config := range map[string]map[string]string{
		"missing host":       {"port": "9030", "database": "analytics"},
		"missing port":       {"host": "doris.internal", "database": "analytics"},
		"invalid port":       {"host": "doris.internal", "port": "70000"},
		"invalid reference":  {"host": "${doris-host}", "port": "9030"},
		"plaintext password": {"host": "doris.internal", "port": "9030", "password": "secret"},
		"unknown field":      {"host": "doris.internal", "port": "9030", "dialect": "DORIS"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := factory.ValidateConfig(config); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
}
