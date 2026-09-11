package main

import (
	"testing"

	"github.com/meaningforge/metis/execution/datasource"
)

func TestDefaultBackendsRegisterCGOFreeRemoteWarehouses(t *testing.T) {
	backends, err := defaultBackends()
	if err != nil {
		t.Fatal(err)
	}
	for backendType, dialect := range map[string]string{"clickhouse": "CLICKHOUSE", "doris": "DORIS"} {
		binding, err := backends.Resolve(datasource.Type(backendType))
		if err != nil {
			t.Fatal(err)
		}
		if binding.Renderer == nil || binding.DriverFactory == nil || string(binding.SQLDialect()) != dialect {
			t.Fatalf("%s Backend = %#v", backendType, binding)
		}
	}
}
