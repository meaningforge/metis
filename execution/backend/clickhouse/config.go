package clickhouse

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"

	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

func validateConfig(config map[string]string) error {
	for key, value := range config {
		if _, _, err := datasource.ParseSecretRef(value); err != nil {
			return fmt.Errorf("ClickHouse config %q is invalid: %w", key, err)
		}
		switch key {
		case "scheme", "address", "database", "username", "password":
		default:
			return fmt.Errorf("unsupported ClickHouse config field %q", key)
		}
	}
	scheme, err := requiredLiteral(config, "scheme")
	if err != nil {
		return err
	}
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("ClickHouse config \"scheme\" must be http or https")
	}
	if _, err := requiredString(config, "address"); err != nil {
		return err
	}
	for _, key := range []string{"database", "username"} {
		if value, exists := config[key]; exists && strings.TrimSpace(value) != value {
			return fmt.Errorf("ClickHouse config %q must be a trimmed string", key)
		}
	}
	if _, exists := config["password"]; exists {
		if _, err := passwordRef(config); err != nil {
			return err
		}
	}
	return nil
}

func clickHouseOptions(config map[string]string, secrets driver.Secrets) (*clickhousedriver.Options, error) {
	scheme, err := requiredLiteral(config, "scheme")
	if err != nil {
		return nil, err
	}
	address, exists, err := driver.ConfigValue(config, secrets, "address")
	if err != nil {
		return nil, err
	}
	if !exists || strings.TrimSpace(address) == "" || strings.TrimSpace(address) != address {
		return nil, fmt.Errorf("ClickHouse config \"address\" is required")
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return nil, fmt.Errorf("ClickHouse config \"address\" must be host:port")
	}
	database, _, err := driver.ConfigValue(config, secrets, "database")
	if err != nil {
		return nil, err
	}
	username, _, err := driver.ConfigValue(config, secrets, "username")
	if err != nil {
		return nil, err
	}
	password := ""
	if _, exists := config["password"]; exists {
		reference, refErr := passwordRef(config)
		if refErr != nil {
			return nil, refErr
		}
		if secrets == nil {
			return nil, fmt.Errorf("ClickHouse password secret is unavailable")
		}
		var found bool
		password, found = secrets.Value(reference)
		if !found || password == "" {
			return nil, fmt.Errorf("ClickHouse password secret is unavailable")
		}
	}
	options := &clickhousedriver.Options{
		Protocol: clickhousedriver.HTTP,
		Addr:     []string{address},
		Auth: clickhousedriver.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
	}
	if scheme == "https" {
		options.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return options, nil
}

func requiredString(config map[string]string, key string) (string, error) {
	value, ok := config[key]
	if !ok || strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("ClickHouse config %q is required", key)
	}
	return value, nil
}

func requiredLiteral(config map[string]string, key string) (string, error) {
	value, err := requiredString(config, key)
	if err != nil {
		return "", err
	}
	if _, referenced, _ := datasource.ParseSecretRef(value); referenced {
		return "", fmt.Errorf("ClickHouse config %q must be a literal", key)
	}
	return value, nil
}

func passwordRef(config map[string]string) (datasource.SecretRef, error) {
	reference, ok, err := datasource.ParseSecretRef(config["password"])
	if err != nil || !ok {
		return datasource.SecretRef{}, fmt.Errorf("ClickHouse config password must be a SecretRef")
	}
	return reference, nil
}
