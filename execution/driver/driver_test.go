package driver_test

import (
	"testing"

	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

type configSecrets map[datasource.SecretRef]string

func (s configSecrets) Value(reference datasource.SecretRef) (string, bool) {
	value, ok := s[reference]
	return value, ok
}

func TestConfigValueResolvesWithoutMutatingConfig(t *testing.T) {
	reference := datasource.SecretRef{Provider: "env", Key: "METIS_DORIS_HOST"}
	config := map[string]string{
		"host":     "${METIS_DORIS_HOST}",
		"database": "analytics",
	}
	host, exists, err := driver.ConfigValue(config, configSecrets{reference: "doris.internal"}, "host")
	if err != nil || !exists || host != "doris.internal" {
		t.Fatalf("resolved host = %q, exists=%t, error=%v", host, exists, err)
	}
	if config["host"] != "${METIS_DORIS_HOST}" {
		t.Fatalf("Config was mutated: %#v", config)
	}
	database, exists, err := driver.ConfigValue(config, nil, "database")
	if err != nil || !exists || database != "analytics" {
		t.Fatalf("literal database = %q, exists=%t, error=%v", database, exists, err)
	}
}

func TestConfigValueFailsClosedOnUnresolvedReference(t *testing.T) {
	config := map[string]string{"host": "${METIS_DORIS_HOST}"}
	if _, _, err := driver.ConfigValue(config, nil, "host"); err == nil {
		t.Fatal("unresolved external value reference was accepted")
	}
}
