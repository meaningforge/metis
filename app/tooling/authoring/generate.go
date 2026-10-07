package authoring

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/clickhouse"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/version"
	"go.yaml.in/yaml/v3"
)

type Report struct {
	SchemaVersion    int                     `json:"schema_version"`
	Status           string                  `json:"status"`
	Project          string                  `json:"project"`
	Model            string                  `json:"model"`
	DataSource       string                  `json:"data_source"`
	Backend          string                  `json:"backend"`
	CatalogDigest    string                  `json:"catalog_digest"`
	MappingDigest    string                  `json:"mapping_digest"`
	GeneratorVersion string                  `json:"generator_version"`
	Mappings         []FieldEvidence         `json:"mappings"`
	ObservedKeys     []KeyEvidence           `json:"observed_keys"`
	ReviewTasks      []string                `json:"review_tasks"`
	Validation       source.ValidationResult `json:"validation"`
}
type FieldEvidence struct {
	Relation  string         `json:"relation"`
	Parts     []string       `json:"parts"`
	Column    Column         `json:"column"`
	Dataset   string         `json:"dataset"`
	Field     string         `json:"field"`
	Datatype  ossie.DataType `json:"datatype"`
	Dimension bool           `json:"dimension"`
}
type KeyEvidence struct {
	Relation string `json:"relation"`
	Key      Key    `json:"key"`
}
type Candidate struct {
	Files  map[string][]byte
	Report Report
}

var logicalIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
var reservedNames = strings.Fields("select from where group order having limit join inner outer left right full on as union case when then else end and or not null true false distinct count sum avg min max con prn aux nul com1 com2 com3 com4 com5 com6 com7 com8 com9 lpt1 lpt2 lpt3 lpt4 lpt5 lpt6 lpt7 lpt8 lpt9")

func validateName(value, location string) error {
	valid := logicalIdentifier.MatchString(value)
	for _, reserved := range reservedNames {
		if strings.EqualFold(value, reserved) {
			valid = false
		}
	}
	if !valid {
		return finding("AUTHORING_INVALID_NAME", location, "use an unreserved ASCII identifier starting with a letter or underscore, at most 64 characters (for example orders_data)")
	}
	return nil
}

func validateMapping(m Mapping, s Snapshot) error {
	if m.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "unsupported mapping version")
	}
	if m.Project != s.Project || m.DataSource != "" && m.DataSource != s.DataSource {
		return finding("AUTHORING_SOURCE_MISMATCH", "mapping", "project and optional data_source must match catalog evidence")
	}
	if err := validateName(m.Model, "model"); err != nil {
		return err
	}
	if len(m.Datasets) == 0 || len(m.Datasets) > 200 {
		return finding("AUTHORING_INVALID_MAPPING", "datasets", "select 1 through 200 datasets")
	}
	names, relations := map[string]bool{}, map[string]bool{}
	for i, ds := range m.Datasets {
		loc := fmt.Sprintf("datasets[%d]", i)
		if err := validateName(ds.Name, loc+".name"); err != nil {
			return err
		}
		if names[strings.ToLower(ds.Name)] || relations[ds.Relation] {
			return finding("AUTHORING_INVALID_MAPPING", loc, "dataset names and selector IDs must be unique")
		}
		names[strings.ToLower(ds.Name)] = true
		relations[ds.Relation] = true
		if len(ds.Fields) == 0 {
			return finding("AUTHORING_INVALID_MAPPING", loc+".fields", "select at least one column explicitly")
		}
		fields, columns := map[string]bool{}, map[string]bool{}
		for j, f := range ds.Fields {
			floc := fmt.Sprintf("%s.fields[%d]", loc, j)
			if err := validateName(f.Name, floc+".name"); err != nil {
				return err
			}
			if fields[strings.ToLower(f.Name)] || columns[f.Column] {
				return finding("AUTHORING_INVALID_MAPPING", floc, "field names and selected columns must be unique within a dataset")
			}
			fields[strings.ToLower(f.Name)] = true
			columns[f.Column] = true
		}
	}
	metrics := map[string]bool{}
	if len(m.StarterMetrics) > 16 {
		return invalid("starter_metrics", "mapping exceeds 16 starter metrics")
	}
	if len(m.StarterMetrics) > 0 && len(m.Datasets) != 1 {
		return finding("AUTHORING_INVALID_MAPPING", "starter_metrics", "row_count currently requires a single selected dataset for unambiguous source binding")
	}
	for i, mtr := range m.StarterMetrics {
		loc := fmt.Sprintf("starter_metrics[%d]", i)
		if err := validateName(mtr.Name, loc+".name"); err != nil {
			return err
		}
		if metrics[strings.ToLower(mtr.Name)] || mtr.Kind != "row_count" || !names[strings.ToLower(mtr.Dataset)] {
			return finding("AUTHORING_INVALID_MAPPING", loc, "only unique, explicit row_count metrics on selected datasets are supported")
		}
		metrics[strings.ToLower(mtr.Name)] = true
		found := false
		for _, ds := range m.Datasets {
			if ds.Name == mtr.Dataset {
				found = true
			}
		}
		if !found {
			return finding("AUTHORING_INVALID_MAPPING", loc, "dataset references are case-sensitive")
		}
	}
	return nil
}

func normalizedMapping(m Mapping) Mapping {
	data, _ := json.Marshal(m)
	var result Mapping
	_ = json.Unmarshal(data, &result)
	for i := range result.Datasets {
		fields := result.Datasets[i].Fields
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	}
	sort.Slice(result.Datasets, func(i, j int) bool { return result.Datasets[i].Name < result.Datasets[j].Name })
	sort.Slice(result.StarterMetrics, func(i, j int) bool { return result.StarterMetrics[i].Name < result.StarterMetrics[j].Name })
	return result
}

// Generate is offline and deterministic. It emits only author-selected columns,
// and never interprets observed keys as semantic cardinality or business meaning.
func Generate(ctx context.Context, s Snapshot, mapping Mapping) (Candidate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	if err := validateSnapshot(s); err != nil {
		return Candidate{}, err
	}
	if err := validateMapping(mapping, s); err != nil {
		return Candidate{}, err
	}
	// Own the input: normalization must not reorder the caller's evidence.
	sdata, _ := json.Marshal(s)
	var owned Snapshot
	_ = json.Unmarshal(sdata, &owned)
	s = owned
	m := normalizedMapping(mapping)
	mapdata, _ := json.Marshal(m)
	placement, _ := json.Marshal(ossie.DataSourcePlacement{Kind: ossie.DataSourceExtensionKind, Name: s.DataSource})
	model := ossie.SemanticModel{Name: m.Model, CustomExtensions: []ossie.CustomExtension{{VendorName: ossie.MetisExtensionVendor, Data: string(placement)}}}
	report := Report{SchemaVersion: SchemaVersion, Status: "candidate", Project: m.Project, Model: m.Model, DataSource: s.DataSource, Backend: s.Backend, CatalogDigest: s.Digest, MappingDigest: digest(mapdata), GeneratorVersion: version.Version, Mappings: []FieldEvidence{}, ObservedKeys: []KeyEvidence{}, ReviewTasks: []string{"Review business metric definitions and aggregation behavior.", "Review relationship cardinality and uniqueness; observed keys are physical metadata only.", "Review timezone, calendar, and null-handling conventions.", "Register the logical DataSource in the eventual deployment and verify its database permissions."}}
	if len(m.StarterMetrics) == 0 {
		report.ReviewTasks = append(report.ReviewTasks, "No metrics were generated; author business metrics explicitly.")
	} else {
		report.ReviewTasks = append(report.ReviewTasks, "Generated row_count metrics count physical rows, not distinct business entities.")
	}
	byID := map[string]Relation{}
	for _, r := range s.Relations {
		byID[r.ID] = r
	}
	for i, selection := range m.Datasets {
		if err := ctx.Err(); err != nil {
			return Candidate{}, err
		}
		loc := fmt.Sprintf("datasets[%d]", i)
		r, ok := byID[selection.Relation]
		if !ok {
			return Candidate{}, finding("AUTHORING_RELATION_NOT_FOUND", loc+".relation", "selector ID is not present in the catalog")
		}
		parts := r.Parts
		if len(r.ResolvedParts) > 0 {
			parts = r.ResolvedParts
		}
		physical, err := physicalSource(s.Backend, parts)
		if err != nil {
			return Candidate{}, finding("AUTHORING_UNSUPPORTED_IDENTIFIER", loc, "relation identifier parts cannot be represented losslessly; inspect an unambiguous object identity")
		}
		ds := ossie.Dataset{Name: selection.Name, Source: physical}
		columns := map[string]Column{}
		for _, c := range r.Columns {
			columns[c.Name] = c
		}
		for j, selected := range selection.Fields {
			floc := fmt.Sprintf("%s.fields[%d]", loc, j)
			col, ok := columns[selected.Column]
			if !ok {
				return Candidate{}, finding("AUTHORING_COLUMN_NOT_FOUND", floc+".column", "selected column is not inventoried for this exact relation")
			}
			if !physicalColumn(col.Name) {
				return Candidate{}, finding("AUTHORING_UNSUPPORTED_IDENTIFIER", floc, "column cannot round-trip through the current expression grammar; exclude it or rename it physically")
			}
			datatype, ok := mapType(s.Backend, col.NativeType)
			if !ok {
				return Candidate{}, finding("AUTHORING_UNSUPPORTED_TYPE", floc, "native type evidence has no faithful supported mapping; exclude this column")
			}
			field := ossie.Field{Name: selected.Name, Datatype: datatype, Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.Dialect(strings.ToUpper(s.Backend)), Expression: selection.Name + ".`" + col.Name + "`"}}}}
			if selected.Dimension {
				field.Dimension = &ossie.Dimension{}
			}
			ds.Fields = append(ds.Fields, field)
			report.Mappings = append(report.Mappings, FieldEvidence{Relation: r.ID, Parts: append([]string(nil), parts...), Column: col, Dataset: selection.Name, Field: selected.Name, Datatype: datatype, Dimension: selected.Dimension})
		}
		for _, key := range r.Keys {
			report.ObservedKeys = append(report.ObservedKeys, KeyEvidence{Relation: r.ID, Key: key})
		}
		model.Datasets = append(model.Datasets, ds)
	}
	for _, mtr := range m.StarterMetrics {
		model.Metrics = append(model.Metrics, ossie.Metric{Name: mtr.Name, Datatype: ossie.DataTypeInteger, Description: "Technical physical row count; not a distinct business entity count.", Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.Dialect(strings.ToUpper(s.Backend)), Expression: "COUNT(*)"}}}})
	}
	// Canonical formatting uses the shared Ossie loader, preserving its grammar.
	body, err := yaml.Marshal(ossie.Document{Version: ossie.SupportedSpecVersion, SemanticModel: []ossie.SemanticModel{model}})
	if err != nil {
		return Candidate{}, err
	}
	body, err = source.FormatDocument(body)
	if err != nil {
		return Candidate{}, finding("AUTHORING_CANDIDATE_INVALID", "model", "generated document failed existing Ossie validation")
	}
	modelPath := "models/" + m.Model + ".ossie.yaml"
	config := &execution.ProjectConfig{SemanticSources: map[string]execution.SemanticSourceConfig{m.Model: {Path: modelPath}}}
	loaded, err := source.LoadProjectDocuments(m.Project, config, []source.SourceDocument{{Source: m.Model, Path: modelPath, Content: body}})
	if err != nil {
		return Candidate{}, finding("AUTHORING_CANDIDATE_INVALID", "project", "generated project failed existing semantic/quality validation")
	}
	report.Validation = source.ValidationResult{SchemaVersion: source.ValidationSchemaVersion, ProjectID: m.Project, ContentDigest: loaded.Bundle.ContentDigest, ManifestDigest: loaded.Manifest.Digest, Valid: true, Publishable: loaded.Quality.Publishable, QualityPublicationThreshold: loaded.Quality.PublicationThreshold, Diagnostics: loaded.Quality.Diagnostics}
	sort.Slice(report.ObservedKeys, func(i, j int) bool {
		a, _ := json.Marshal(report.ObservedKeys[i])
		b, _ := json.Marshal(report.ObservedKeys[j])
		return string(a) < string(b)
	})
	project, err := yaml.Marshal(config)
	if err != nil {
		return Candidate{}, err
	}
	rdata, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return Candidate{}, err
	}
	guide := fmt.Sprintf("# Review this generated candidate\n\nProject: %s. Model: %s. Logical DataSource: %s.\n\nThe catalog is author-supplied evidence, not proof of database access.\nRead authoring-report.json and review the physical mappings, pending decisions,\nand existing validation findings before adoption. Publishable only describes\na quality threshold.\n\nFrom this candidate directory:\n\n```sh\nmetis project validate --project %s --config ./project.yaml\nmetis model inspect --model ./%s\n```\n\nFor the eventual deployment, register this Project and apply the logical\nDataSource named %s with independently managed connection configuration.\nThe model-level placement must resolve to that source. Author business metrics\nand relationships explicitly, then compile and run an authorized test query.\nmetis catalog inspect can capture fresh Doris/ClickHouse metadata separately.\nMetadata capture does not prove SELECT permission. Generation itself remains\noffline and has not contacted or validated a database. Runtime validation\nunder RFC-0088 remains a proposal. After any manual model edits, validate and\nreview them independently; this generation report is not approval of those edits.\n", m.Project, m.Model, s.DataSource, "'"+strings.ReplaceAll(m.Project, "'", "'\\''")+"'", modelPath, s.DataSource)
	return Candidate{Files: map[string][]byte{"project.yaml": project, modelPath: body, "authoring-report.json": append(rdata, '\n'), "GETTING_STARTED.md": []byte(guide)}, Report: report}, nil
}

func physicalSource(backend string, parts []string) (string, error) {
	if backend == "clickhouse" && len(parts) != 2 || backend == "doris" && (len(parts) < 1 || len(parts) > 3) {
		return "", fmt.Errorf("unsupported identifier arity")
	}
	for _, part := range parts {
		if !boundedName(part) || strings.ContainsAny(part, ".`\"\\") || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return "", fmt.Errorf("unsupported identifier part")
		}
	}
	value := strings.Join(parts, ".")
	var rendered string
	var err error
	if backend == "doris" {
		rendered, err = (doris.Renderer{}).QuoteSource(value)
	} else {
		rendered, err = (clickhouse.Renderer{}).QuoteSource(value)
	}
	expected := make([]string, len(parts))
	for i, part := range parts {
		expected[i] = "`" + part + "`"
	}
	if err != nil || rendered != strings.Join(expected, ".") {
		return "", fmt.Errorf("source identity changed during rendering")
	}
	return value, nil
}
func physicalColumn(name string) bool {
	return boundedName(name) && !strings.ContainsAny(name, ".`\"\\") && strings.IndexFunc(name, unicode.IsControl) < 0
}

func mapType(backend string, n NativeType) (ossie.DataType, bool) {
	name := strings.ToUpper(n.Name)
	if n.Length != nil && name != "CHAR" && name != "VARCHAR" && name != "STRING" && name != "FIXEDSTRING" {
		return "", false
	}
	if n.Timezone != nil && name != "DATETIME" && name != "DATETIME64" {
		return "", false
	}
	decimal := false
	if backend == "doris" {
		switch name {
		case "CHAR", "VARCHAR", "STRING":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeString, true
			}
		case "BOOLEAN", "BOOL":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeBoolean, true
			}
		case "TINYINT", "SMALLINT", "INT", "INTEGER", "BIGINT", "LARGEINT":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeInteger, true
			}
		case "FLOAT", "DOUBLE":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeFloat, true
			}
		case "DECIMAL", "DECIMALV2", "DECIMALV3":
			decimal = true
		case "DATE", "DATEV2":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeDate, true
			}
		case "DATETIME", "DATETIMEV2":
			if n.Timezone == nil && n.Scale == nil && (n.Precision == nil || *n.Precision <= 6) {
				return ossie.DataTypeDateTime, true
			}
		}
	} else if backend == "clickhouse" {
		switch name {
		case "STRING", "FIXEDSTRING":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeString, true
			}
		case "BOOL":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeBoolean, true
			}
		case "INT8", "INT16", "INT32", "INT64", "INT128", "INT256", "UINT8", "UINT16", "UINT32", "UINT64", "UINT128", "UINT256":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeInteger, true
			}
		case "FLOAT32", "FLOAT64":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeFloat, true
			}
		case "DECIMAL":
			decimal = true
		case "DATE", "DATE32":
			if n.Precision == nil && n.Scale == nil {
				return ossie.DataTypeDate, true
			}
		case "DATETIME", "DATETIME64":
			if n.Scale == nil && (n.Precision == nil || *n.Precision <= 9) {
				if n.Timezone != nil {
					return ossie.DataTypeDateTimeTz, true
				}
				return ossie.DataTypeDateTime, true
			}
		}
	}
	if decimal && n.Precision != nil && n.Scale != nil && *n.Precision > 0 && *n.Precision <= 38 && *n.Scale <= *n.Precision {
		return ossie.DataTypeDecimal, true
	}
	return "", false
}
