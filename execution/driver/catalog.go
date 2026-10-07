package driver

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxCatalogRelations = 200
const MaxCatalogColumns = 10000
const MaxCatalogBytes = 10 << 20

// CatalogReference is an exact physical identity, never SQL or a wildcard.
// Online inspection requires an explicit database (and optional Doris catalog).
type CatalogReference struct {
	ID    string   `json:"id" yaml:"id"`
	Parts []string `json:"parts" yaml:"parts"`
}

type CatalogNativeType struct {
	Name      string  `json:"name"`
	Precision *int    `json:"precision,omitempty"`
	Scale     *int    `json:"scale,omitempty"`
	Length    *int    `json:"length,omitempty"`
	Timezone  *string `json:"timezone,omitempty"`
}
type CatalogColumn struct {
	Name       string            `json:"name"`
	NativeType CatalogNativeType `json:"native_type"`
	Nullable   string            `json:"nullable"`
}
type CatalogRelation struct {
	Reference       CatalogReference `json:"reference"`
	Outcome         string           `json:"outcome"`
	ColumnsComplete bool             `json:"columns_complete"`
	Columns         []CatalogColumn  `json:"columns"`
}
type CatalogLimits struct {
	MaxColumns int
	MaxBytes   int
}

// CatalogInspector is an optional Executor capability. Implementations inspect
// one exact relation, use ctx for every I/O, bound results before accumulating,
// and never return row samples, SQL definitions, credentials, or comments.
// A failed operation is not evidence that an object is absent.
type CatalogInspector interface {
	DescribeRelation(context.Context, CatalogReference, CatalogLimits) (CatalogRelation, error)
}

func ValidateCatalogReference(backend string, ref CatalogReference) error {
	maxParts := 2
	if backend == "doris" || backend == "duckdb" {
		maxParts = 3
	} else if backend != "clickhouse" {
		return fmt.Errorf("unsupported catalog backend")
	}
	if !catalogName(ref.ID) || len(ref.Parts) < 2 || len(ref.Parts) > maxParts {
		return fmt.Errorf("invalid catalog reference")
	}
	for _, part := range ref.Parts {
		if !catalogName(part) || strings.ContainsAny(part, ".`\"'\\*?;") {
			return fmt.Errorf("invalid physical identifier")
		}
	}
	return nil
}
func catalogName(value string) bool {
	if value == "" || len(value) > 256 || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
