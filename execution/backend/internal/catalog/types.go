package catalog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/execution/driver"
)

var simpleType = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)(?:\((.*)\))?$`)
var timezoneValue = regexp.MustCompile(`^'([A-Za-z0-9_+/.-]+)'$`)

func integer(value string) (*int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 || n > driver.MaxCatalogBytes {
		return nil, fmt.Errorf("invalid native type parameter")
	}
	return &n, nil
}

// ParseDoris retains unsupported native evidence instead of guessing a mapping.
func ParseDoris(value string) (driver.CatalogNativeType, string, error) {
	value = strings.TrimSpace(value)
	match := simpleType.FindStringSubmatch(value)
	if match != nil {
		switch strings.ToUpper(match[1]) {
		case "CHAR", "VARCHAR", "STRING", "BOOLEAN", "BOOL", "TINYINT", "SMALLINT", "INT", "INTEGER", "BIGINT", "LARGEINT", "FLOAT", "DOUBLE", "DECIMAL", "DECIMALV2", "DECIMALV3", "DATE", "DATEV2", "DATETIME", "DATETIMEV2":
			value = strings.ToUpper(value)
		}
	}
	return parse(value, false)
}
func ParseClickHouse(value string) (driver.CatalogNativeType, string, error) {
	value = strings.TrimSpace(value)
	nullable := "not_null"
	// LowCardinality is storage encoding, not a different base logical type.
	for i := 0; i < 2; i++ {
		if strings.HasPrefix(value, "Nullable(") && strings.HasSuffix(value, ")") {
			nullable = "nullable"
			value = value[len("Nullable(") : len(value)-1]
		} else if strings.HasPrefix(value, "LowCardinality(") && strings.HasSuffix(value, ")") {
			value = value[len("LowCardinality(") : len(value)-1]
		} else {
			break
		}
	}
	native, _, err := parse(value, true)
	return native, nullable, err
}

// ParseDuckDB preserves unsupported complex/native type text and timestamp
// units. TIMESTAMPTZ identifies a zoned type, not a guessed configured timezone.
func ParseDuckDB(value string) (driver.CatalogNativeType, string, error) {
	value = strings.TrimSpace(value)
	native, nullable, err := parse(value, false)
	if err != nil {
		return native, nullable, err
	}
	precision, known := map[string]int{"TIMESTAMP": 6, "TIMESTAMP_S": 0, "TIMESTAMP_MS": 3, "TIMESTAMP_NS": 9, "TIMESTAMP WITH TIME ZONE": 6, "TIMESTAMPTZ": 6}[strings.ToUpper(value)]
	if known {
		native.Name = strings.ToUpper(value)
		native.Precision = &precision
	}
	return native, nullable, nil
}
func parse(value string, clickhouse bool) (driver.CatalogNativeType, string, error) {
	native := driver.CatalogNativeType{Name: value}
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
		return native, "", fmt.Errorf("invalid native type")
	}
	match := simpleType.FindStringSubmatch(value)
	if match == nil {
		return native, "unknown", nil
	}
	name := match[1]
	args := []string{}
	if match[2] != "" {
		args = strings.Split(match[2], ",")
	}
	upper := strings.ToUpper(name)
	var err error
	switch upper {
	case "DECIMAL", "DECIMALV2", "DECIMALV3":
		if len(args) != 2 {
			return native, "", fmt.Errorf("decimal requires precision and scale")
		}
		native.Name = name
		native.Precision, err = integer(args[0])
		if err == nil {
			native.Scale, err = integer(args[1])
		}
	case "DECIMAL32", "DECIMAL64", "DECIMAL128", "DECIMAL256":
		if !clickhouse || len(args) != 1 {
			return native, "unknown", nil
		}
		precision := map[string]int{"DECIMAL32": 9, "DECIMAL64": 18, "DECIMAL128": 38, "DECIMAL256": 76}[upper]
		native.Name, native.Precision = "Decimal", &precision
		native.Scale, err = integer(args[0])
	case "CHAR", "VARCHAR", "FIXEDSTRING":
		if len(args) != 1 {
			return native, "unknown", nil
		}
		native.Name = name
		native.Length, err = integer(args[0])
	case "DATETIME", "DATETIMEV2", "DATETIME64":
		native.Name = name
		if clickhouse {
			if upper == "DATETIME64" {
				if len(args) < 1 || len(args) > 2 {
					return native, "", fmt.Errorf("invalid datetime precision")
				}
				native.Precision, err = integer(args[0])
				args = args[1:]
			}
			if len(args) > 1 {
				return native, "", fmt.Errorf("invalid timezone evidence")
			}
			if len(args) == 1 {
				match := timezoneValue.FindStringSubmatch(strings.TrimSpace(args[0]))
				if match == nil {
					return native, "", fmt.Errorf("invalid timezone evidence")
				}
				native.Timezone = &match[1]
			}
		} else {
			if len(args) > 1 {
				return native, "", fmt.Errorf("invalid datetime precision")
			}
			if len(args) == 1 {
				native.Precision, err = integer(args[0])
			}
		}
	default:
		// Complex/unsupported types remain opaque evidence; semantic init requires
		// explicit exclusion when it cannot represent them without a cast.
	}
	return native, "unknown", err
}
