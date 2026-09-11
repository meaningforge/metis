package artifact

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

// OutputColumnKind identifies the semantic role of one compiled output column.
type OutputColumnKind string

const (
	OutputDimension OutputColumnKind = "dimension"
	OutputMetric    OutputColumnKind = "metric"
)

// OutputColumn describes one logical result column in physical query order.
// Datatype is the declared Ossie semantic datatype, not a database wire type.
type OutputColumn struct {
	Name     string           `json:"name"`
	Kind     OutputColumnKind `json:"kind"`
	Datatype ossie.DataType   `json:"datatype,omitempty"`
	Grain    *query.TimeGrain `json:"grain,omitempty"`
}

// OutputSchema is the target-neutral result contract returned with a compiled
// physical query. Columns are ordered exactly as the physical query output.
type OutputSchema struct {
	Columns []OutputColumn `json:"columns"`
}
