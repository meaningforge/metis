package sql

// SQLDialect identifies one physical SQL language. It never identifies a
// database instance or execution route.
type SQLDialect string

// QueryParameter is one Renderer-produced SQL parameter.
type QueryParameter struct {
	Name string `json:"name,omitempty"`
	// Value has a closed runtime domain: nil, string, bool, built-in integer
	// and finite floating-point types, json.Number, or []byte.
	Value any `json:"value"`
}

// SQLQuery is one complete rendered SQL query. SQL retains placeholders;
// execution backends pass Parameters separately to their database drivers.
// Exporters preserve both SQL and Parameters without interpolating values.
type SQLQuery struct {
	Dialect    SQLDialect       `json:"dialect"`
	SQL        string           `json:"sql"`
	Parameters []QueryParameter `json:"parameters,omitempty"`
}
