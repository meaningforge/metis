package doris

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

func validateConfig(config map[string]string) error {
	for key := range config {
		if _, _, err := datasource.ParseSecretRef(config[key]); err != nil {
			return fmt.Errorf("Doris config %q is invalid: %w", key, err)
		}
		switch key {
		case "host", "port", "database", "username", "password":
		default:
			return fmt.Errorf("unsupported Doris config field %q", key)
		}
	}
	if _, err := requiredString(config, "host"); err != nil {
		return err
	}
	if _, err := validatedPort(config); err != nil {
		return err
	}
	for _, key := range []string{"database", "username"} {
		if value, exists := config[key]; exists {
			if strings.TrimSpace(value) != value {
				return fmt.Errorf("Doris config %q must be a trimmed string", key)
			}
		}
	}
	if _, exists := config["password"]; exists {
		if _, err := passwordRef(config); err != nil {
			return err
		}
	}
	return nil
}

func validatedPort(config map[string]string) (string, error) {
	value, err := requiredString(config, "port")
	if err != nil {
		return "", err
	}
	if _, isReference, parseErr := datasource.ParseSecretRef(value); parseErr != nil {
		return "", fmt.Errorf("Doris config \"port\" is invalid: %w", parseErr)
	} else if isReference {
		return value, nil
	}
	port, parseErr := strconv.Atoi(value)
	if parseErr != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("Doris config \"port\" must be an integer between 1 and 65535")
	}
	return value, nil
}

func connectionAddress(config map[string]string, secrets driver.Secrets) (string, error) {
	host, exists, err := driver.ConfigValue(config, secrets, "host")
	if err != nil {
		return "", err
	}
	if !exists || strings.TrimSpace(host) == "" || strings.TrimSpace(host) != host {
		return "", fmt.Errorf("Doris config \"host\" is required")
	}
	port, _, err := driver.ConfigValue(config, secrets, "port")
	if err != nil {
		return "", err
	}
	parsedPort, parseErr := strconv.Atoi(port)
	if parseErr != nil || parsedPort < 1 || parsedPort > 65535 {
		return "", fmt.Errorf("Doris config \"port\" must be an integer between 1 and 65535")
	}
	return net.JoinHostPort(host, port), nil
}

func requiredString(config map[string]string, key string) (string, error) {
	value, ok := config[key]
	if !ok || strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("Doris config %q is required", key)
	}
	return value, nil
}

func passwordRef(config map[string]string) (datasource.SecretRef, error) {
	reference, ok, err := datasource.ParseSecretRef(config["password"])
	if err != nil || !ok {
		return datasource.SecretRef{}, fmt.Errorf("Doris config password must be a SecretRef")
	}
	return reference, nil
}
