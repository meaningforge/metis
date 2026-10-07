package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/tooling/authoring"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/version"
)

type Report struct {
	SchemaVersion   int          `json:"schema_version"`
	Mode            string       `json:"mode"`
	Project         string       `json:"project"`
	MetisVersion    string       `json:"metis_version"`
	CandidateDigest string       `json:"candidate_digest,omitempty"`
	InventoryDigest string       `json:"inventory_digest"`
	OfflineChecked  bool         `json:"offline_checked"`
	Complete        bool         `json:"complete"`
	Passed          bool         `json:"passed"`
	Code            string       `json:"code,omitempty"`
	Cases           []CaseReport `json:"cases"`
}
type CaseReport struct {
	ID             string `json:"id"`
	CompileChecked bool   `json:"compile_checked"`
	CatalogChecked bool   `json:"catalog_checked"`
	EnginePrepared bool   `json:"engine_prepared"`
	Outcome        string `json:"outcome"`
	Code           string `json:"code,omitempty"`
	Backend        string `json:"backend,omitempty"`
	Method         string `json:"method,omitempty"`
}

func Run(ctx context.Context, config, project string, inventory Inventory, options ...bootstrap.RuntimeOption) (Report, error) {
	// Revalidate programmatic input and take ownership before assembly.
	data, err := json.Marshal(inventory)
	if err != nil {
		return Report{}, fmt.Errorf("invalid inventory")
	}
	inventory, err = ParseInventory(data, project)
	if err != nil {
		return Report{}, err
	}
	digest := sha256.Sum256(data)
	report := Report{SchemaVersion: 1, Mode: "online", Project: project, MetisVersion: version.Version, InventoryDigest: hex.EncodeToString(digest[:])}
	for _, c := range inventory.Queries {
		report.Cases = append(report.Cases, CaseReport{ID: c.ID, Outcome: "not_run", Code: "preparation_not_completed"})
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	runtime, err := bootstrap.LoadValidationRuntime(ctx, config, project, options...)
	if err != nil {
		report.Code = safeCode(err)
		return report, nil
	}
	defer runtime.Close(context.Background())
	generation := runtime.Current(project)
	if generation == nil || runtime.Execution == nil {
		report.Code = "runtime_unavailable"
		return report, nil
	}
	report.OfflineChecked = true
	report.CandidateDigest = generation.ContentDigest()
	prepared := make([]semantic.PreparedValidation, len(inventory.Queries))
	for i, c := range inventory.Queries {
		prepared[i], err = generation.QueryMetrics.PrepareValidation(ctx, c.Query)
		if err != nil {
			report.Cases[i].Outcome = "failed"
			report.Cases[i].Code = safeCode(err)
			return report, nil
		}
		report.Cases[i].CompileChecked = true
		report.Cases[i].Code = "online_checks_not_started"
		report.Cases[i].Backend = string(prepared[i].Route.Backend.Type)
	}
	for i, work := range prepared {
		c := &report.Cases[i]
		if c.Backend != "doris" && c.Backend != "clickhouse" && c.Backend != "duckdb" {
			c.Outcome = "unsupported"
			c.Code = "backend_unsupported"
			continue
		}
		// Only demonstrated non-executing shapes are eligible for online I/O.
		if !runner.SupportsCompiledValidation(work.Route, work.Compiled) {
			c.Outcome = "unsupported"
			c.Code = "parameter_validation_unsupported"
			continue
		}
		refs := make([]driver.CatalogReference, 0, len(work.Relations))
		// Several semantic datasets may refer to one physical relation.
		seen := map[string]bool{}
		for _, relation := range work.Relations {
			keyBytes, _ := json.Marshal(relation.Reference.Parts)
			key := string(keyBytes)
			if !seen[key] {
				ref := relation.Reference
				ref.ID = fmt.Sprintf("relation_%d", len(refs))
				refs = append(refs, ref)
				seen[key] = true
			}
		}
		metadata, err := runtime.Execution.DescribeRelations(ctx, work.Route.Name, refs)
		if err != nil {
			c.Outcome = "unavailable"
			c.Code = safeCode(err)
			continue
		}
		matched, unknown := true, false
		for _, relation := range work.Relations {
			var observed *driver.CatalogRelation
			for j := range metadata {
				left, _ := json.Marshal(metadata[j].Reference.Parts)
				right, _ := json.Marshal(relation.Reference.Parts)
				if string(left) == string(right) {
					observed = &metadata[j]
					break
				}
			}
			if observed == nil {
				matched = false
				unknown = true
				c.Code = "catalog_incomplete"
				break
			}
			for _, field := range relation.Columns {
				found := false
				for _, column := range observed.Columns {
					if !columnMatches(c.Backend, column.Name, field.Name) {
						continue
					}
					found = true
					if field.Datatype != "" {
						n := column.NativeType
						actual, known := authoring.NativeDatatype(c.Backend, authoring.NativeType{Name: n.Name, Precision: n.Precision, Scale: n.Scale, Length: n.Length, Timezone: n.Timezone})
						if !known {
							matched = false
							unknown = true
							c.Code = "source_type_unknown"
						} else if actual != field.Datatype {
							matched = false
							c.Code = "source_type_mismatch"
						}
					}
				}
				if !found {
					matched = false
					c.Code = "source_dependency_mismatch"
				}
			}
		}
		c.CatalogChecked = !unknown
		if !matched {
			c.Outcome = "failed"
			if unknown {
				c.Outcome = "unsupported"
			}
			continue
		}
		c.CatalogChecked = true
		evidence, err := runtime.Execution.ValidateCompiled(ctx, work.Route, work.Compiled)
		if err != nil {
			c.Outcome = "unavailable"
			c.Code = safeCode(err)
			continue
		}
		c.Method = evidence.Method
		if evidence.Outcome != "accepted" {
			c.Outcome = "unsupported"
			c.Code = "engine_validation_unsupported"
			continue
		}
		c.EnginePrepared = true
		c.Outcome = "passed"
		c.Code = ""
	}
	report.Complete = true
	report.Passed = true
	for _, c := range report.Cases {
		if c.Outcome != "passed" {
			report.Passed = false
		}
		if c.Outcome == "unsupported" || c.Outcome == "unavailable" || c.Outcome == "not_run" {
			report.Complete = false
		}
	}
	return report, nil
}

// DuckDB folds ASCII identifiers even when quoted. Unicode case folding would
// incorrectly equate distinct native names, so retain non-ASCII spelling.
func columnMatches(backend, observed, wanted string) bool {
	if backend != "duckdb" {
		return observed == wanted
	}
	fold := func(s string) string {
		b := []byte(s)
		for i, v := range b {
			if v >= 'A' && v <= 'Z' {
				b[i] = v + ('a' - 'A')
			}
		}
		return string(b)
	}
	return fold(observed) == fold(wanted)
}

func safeCode(err error) string {
	var semanticErr *serrors.Error
	if errors.As(err, &semanticErr) {
		return string(semanticErr.Code)
	}
	var executionErr *runner.ExecutionError
	if errors.As(err, &executionErr) {
		return string(executionErr.Code)
	}
	return "validation_unavailable"
}

// WriteReport publishes a fresh owner-only file without overwriting inputs,
// symlinks or a competing writer. A hard link atomically claims the final name.
func WriteReport(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil || len(data) > driver.MaxCatalogBytes {
		return fmt.Errorf("report exceeds output bounds")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".metis-validation-*")
	if err != nil {
		return fmt.Errorf("cannot create validation report temporary file")
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(data, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("cannot finish validation report")
	}
	if err := os.Link(file.Name(), path); err != nil {
		return fmt.Errorf("cannot publish report; use a fresh output path")
	}
	return nil
}
