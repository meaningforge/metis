package authoring

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
)

type Selectors struct {
	SchemaVersion int                       `json:"schema_version" yaml:"schema_version"`
	Relations     []driver.CatalogReference `json:"relations" yaml:"relations"`
}

func LoadSelectors(path string) (Selectors, error) {
	data, err := readBounded(path, MaxMappingBytes)
	if err != nil {
		return Selectors{}, err
	}
	return ParseSelectors(data)
}
func ParseSelectors(data []byte) (Selectors, error) {
	var result Selectors
	if err := decodeStrict(data, &result, MaxMappingBytes); err != nil {
		return result, err
	}
	if result.SchemaVersion != SchemaVersion || len(result.Relations) == 0 || len(result.Relations) > driver.MaxCatalogRelations {
		return Selectors{}, invalid("relations", "expected version 1 and 1 through 200 exact relation selectors")
	}
	ids, physical := map[string]bool{}, map[string]bool{}
	for _, ref := range result.Relations {
		// Backend-specific part count is tightened by Runner after source selection.
		parts, _ := json.Marshal(ref.Parts)
		if driver.ValidateCatalogReference("doris", ref) != nil || ids[ref.ID] || physical[string(parts)] {
			return Selectors{}, invalid("relations", "selectors require unique IDs and exact qualified identifiers")
		}
		ids[ref.ID], physical[string(parts)] = true, true
	}
	return result, nil
}

// InspectCatalog converts only bounded, matched driver evidence to the existing
// offline schema. It performs no semantic inference or model activation.
func InspectCatalog(ctx context.Context, execution *runner.Runner, project, dataSource string, selectors Selectors) (Snapshot, error) {
	resolved, err := execution.ResolveDataSource(dataSource)
	if err != nil {
		return Snapshot{}, err
	}
	metadata, err := execution.DescribeRelations(ctx, dataSource, selectors.Relations)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{SchemaVersion: SchemaVersion, Project: project, DataSource: dataSource, Backend: string(resolved.Source.Type), Relations: make([]Relation, 0, len(metadata))}
	for _, relation := range metadata {
		r := Relation{ID: relation.Reference.ID, Parts: relation.Reference.Parts, Outcome: relation.Outcome, ColumnsComplete: relation.ColumnsComplete, Columns: make([]Column, 0, len(relation.Columns))}
		for _, c := range relation.Columns {
			n := c.NativeType
			r.Columns = append(r.Columns, Column{Name: c.Name, Nullable: c.Nullable, NativeType: NativeType{Name: n.Name, Precision: n.Precision, Scale: n.Scale, Length: n.Length, Timezone: n.Timezone}})
		}
		sort.Slice(r.Columns, func(i, j int) bool { return r.Columns[i].Name < r.Columns[j].Name })
		s.Relations = append(s.Relations, r)
	}
	sort.Slice(s.Relations, func(i, j int) bool { return s.Relations[i].ID < s.Relations[j].ID })
	s.Digest, err = CatalogDigest(s)
	if err != nil {
		return Snapshot{}, invalid("catalog", "cannot hash catalog evidence")
	}
	if err := validateSnapshot(s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// WriteSnapshot writes privately, validates the exact bytes consumed by project
// init, and atomically publishes without replacing files or symlinks.
func WriteSnapshot(ctx context.Context, output string, snapshot Snapshot) error {
	if ctx == nil {
		ctx = context.Background()
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return invalid("catalog", "cannot encode catalog evidence")
	}
	data = append(data, '\n')
	if _, err := ParseSnapshot(data); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(output)
	if err != nil || output == "" {
		return invalid("output", "output file is required")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return invalid("output", "parent directory must exist")
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	file, err := os.CreateTemp(parent, ".metis-catalog-*")
	if err != nil {
		return invalid("output", "cannot create private catalog staging file")
	}
	temporary := file.Name()
	defer os.Remove(temporary) // only this invocation's generated temporary file
	_, writeErr := file.Write(data)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return invalid("output", "cannot finish catalog staging file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := renameExclusive(temporary, absolute); err != nil {
		return invalid("output", "cannot publish catalog without replacing an existing path")
	}
	return nil
}
