// Package execution is the transitional home of deployment configuration
// loading. Query running lives in execution/runner; public DataSource, Driver,
// and Backend extension contracts live in execution/datasource,
// execution/driver, and execution/backend. Concrete warehouse Backends are
// composed by the executable, not this package.
package execution
