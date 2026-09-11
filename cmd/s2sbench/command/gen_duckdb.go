//go:build duckdb

package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/duckdb/duckdb-go/v2"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	service "github.com/meaningforge/metis/app/service/semantic"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	duckdbfixture "github.com/meaningforge/metis/cmd/s2sbench/bench/duckdbfixture"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

const (
	ossieTPCDSRevision = "88e0011148283302c9a04cd0287e00e0b9d87354"
	ossieTPCDSURL      = "https://raw.githubusercontent.com/apache/ossie/" + ossieTPCDSRevision + "/examples/tpcds_semantic_model.yaml"
	ossieTPCDSProvider = "ossie-tpcds-example"
	ossieTPCDSVersion  = "v3"
	tPCDSSpecification = "TPC-DS 2.10.0"
)

type genOptions struct {
	Provider   string
	Engine     string
	Scale      float64
	Output     string
	OssieModel string
}

type workloadGenerator interface {
	Generate(context.Context, genOptions, io.Writer) error
}

type workloadGeneratorFunc func(context.Context, genOptions, io.Writer) error

func (f workloadGeneratorFunc) Generate(ctx context.Context, options genOptions, stdout io.Writer) error {
	return f(ctx, options, stdout)
}

var workloadGenerators = map[string]workloadGenerator{
	ossieTPCDSProvider: workloadGeneratorFunc(generateOssieTPCDSWorkload),
}

func newGenCommand(stdout io.Writer) *cobra.Command {
	options := genOptions{Provider: ossieTPCDSProvider, Engine: "duckdb", Scale: 0.01, Output: ".workload"}
	command := &cobra.Command{
		Use:   "gen",
		Short: "Generate a standards-backed S2SBench workload",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return generateWorkload(context.Background(), options, stdout)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Provider, "provider", options.Provider, "registered workload provider")
	flags.StringVar(&options.Engine, "engine", options.Engine, "database used to execute reference and Metis SQL")
	flags.Float64Var(&options.Scale, "scale", options.Scale, "TPC-DS scale factor passed unchanged to dsdgen")
	flags.StringVar(&options.Output, "output", options.Output, "new workload bundle directory (default .workload in the current directory)")
	flags.StringVar(&options.OssieModel, "ossie-model", "", "local Apache Ossie-compatible TPC-DS semantic template; pinned Apache source is used by default")
	return command
}

func generateWorkload(ctx context.Context, options genOptions, stdout io.Writer) error {
	options.Provider = strings.TrimSpace(options.Provider)
	generator, ok := workloadGenerators[options.Provider]
	if !ok {
		return fmt.Errorf("workload provider %q is not registered", options.Provider)
	}
	return generator.Generate(ctx, options, stdout)
}

func generateOssieTPCDSWorkload(ctx context.Context, options genOptions, stdout io.Writer) (returnErr error) {
	options.Engine = strings.ToLower(strings.TrimSpace(options.Engine))
	if options.Engine != "duckdb" {
		return fmt.Errorf("engine %q is not registered for workload generation", options.Engine)
	}
	if options.Scale <= 0 || options.Scale > 100 {
		return fmt.Errorf("--scale must be greater than 0 and at most 100")
	}
	outputValue := strings.TrimSpace(options.Output)
	if outputValue == "" {
		outputValue = ".workload"
	}
	output, err := filepath.Abs(outputValue)
	if err != nil {
		return fmt.Errorf("resolve workload output %q: %w", outputValue, err)
	}
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("output directory %q already exists; refusing to overwrite a workload", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	staging := output + ".partial"
	if _, err := os.Stat(staging); err == nil {
		return fmt.Errorf("partial workload directory %q already exists", staging)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(staging)
		}
	}()

	model, source, revision, err := loadTPCDSModel(options.OssieModel)
	if err != nil {
		return err
	}
	for _, dir := range []string{"models", "data/csv", "data/parquet", "cases"} {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o755); err != nil {
			return err
		}
	}
	databaseRelative := "data/tpcds.duckdb"
	databasePath := filepath.Join(staging, filepath.FromSlash(databaseRelative))
	generatorVersion, tables, err := generateStandardTPCDS(ctx, databasePath, options.Scale)
	if err != nil {
		return err
	}
	physicalCatalog, err := readDuckDBCatalog(ctx, databasePath, "tpcds", "public", tPCDSSpecification, generatorVersion, time.Now().UTC().Truncate(time.Second))
	if err != nil {
		return err
	}
	catalog, err := scopeTPCDSCatalogToOssieExample(model, physicalCatalog)
	if err != nil {
		return fmt.Errorf("scope TPC-DS catalog to Apache Ossie example: %w", err)
	}
	model, err = buildTPCDSOssieModel(model, catalog)
	if err != nil {
		return fmt.Errorf("generate Ossie TPC-DS semantic model from DuckDB catalog: %w", err)
	}
	modelRelative := "models/ossie-tpcds-example.ossie.yaml"
	if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(modelRelative)), model, 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(staging, "catalog"), 0o755); err != nil {
		return err
	}
	catalogRelative := "catalog/catalog.json"
	catalogBody, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(catalogRelative)), append(catalogBody, '\n'), 0o644); err != nil {
		return err
	}
	dataAssets, err := exportTPCDSData(ctx, staging, databasePath, tables)
	if err != nil {
		return err
	}
	schemaRelative := "data/schema.sql"
	if err := writeTPCDSSchema(ctx, databasePath, filepath.Join(staging, filepath.FromSlash(schemaRelative))); err != nil {
		return err
	}
	loaderRelative := "data/load-duckdb.sql"
	if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(loaderRelative)), []byte(tpcdsLoaderSQL(tables)), 0o644); err != nil {
		return err
	}

	definition := fixtures.Definition{ID: "ossie_tpcds", Project: "tpcds", Model: "tpcds_retail_model", Document: model}
	cases, err := freezeTPCDSOracles(ctx, filepath.Join(staging, ".oracle-runtime"), staging, databasePath, definition)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(staging, ".oracle-runtime")); err != nil {
		return err
	}
	asset := func(relative, format string) (workload.Asset, error) {
		digest, err := workload.FileDigest(filepath.Join(staging, filepath.FromSlash(relative)))
		return workload.Asset{Path: relative, SHA256: digest, Format: format}, err
	}
	modelAsset, err := asset(modelRelative, "ossie")
	if err != nil {
		return err
	}
	databaseAsset, err := asset(databaseRelative, "duckdb")
	if err != nil {
		return err
	}
	schemaAsset, err := asset(schemaRelative, "sql")
	if err != nil {
		return err
	}
	loaderAsset, err := asset(loaderRelative, "sql")
	if err != nil {
		return err
	}
	bundle := workload.Bundle{
		SchemaVersion: workload.SchemaVersion, Name: "ossie-tpcds-example", Provider: options.Provider, ProviderVersion: ossieTPCDSVersion,
		Engine: options.Engine, Scale: options.Scale,
		DataProvenance: workload.DataProvenance{Classification: "benchmark-derived", Specification: tPCDSSpecification, Generator: "DuckDB tpcds dsdgen", Version: generatorVersion},
		Source:         source, SourceRevision: revision, SourceSHA256: modelAsset.SHA256,
		Models: []workload.ModelAsset{{Asset: modelAsset, Project: "tpcds", Model: "tpcds_retail_model"}}, Data: dataAssets,
		Database: databaseAsset, Schema: schemaAsset, Loader: loaderAsset, Cases: cases,
	}
	if err := bundle.Seal(); err != nil {
		return err
	}
	manifest, err := os.Create(filepath.Join(staging, workload.ManifestFile))
	if err != nil {
		return err
	}
	encoder := jsonEncoder(manifest)
	if err := encoder.Encode(bundle); err != nil {
		_ = manifest.Close()
		return err
	}
	if err := manifest.Close(); err != nil {
		return err
	}
	if _, err := workload.Load(staging); err != nil {
		return fmt.Errorf("verify generated workload: %w", err)
	}
	if err := os.Rename(staging, output); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "S2SBench workload generated: %s\n", output)
	fmt.Fprintf(stdout, "provider=%s specification=%q classification=benchmark-derived generator_version=%s scale=%g tables=%d cases=%d\n", options.Provider, tPCDSSpecification, generatorVersion, options.Scale, len(tables), len(cases))
	fmt.Fprintf(stdout, "database=%s csv_tables=%d parquet_tables=%d semantic_tables=%d knowledge=not-generated digest=%s\n", filepath.Join(output, databaseRelative), len(tables), len(tables), len(catalog.Tables), bundle.Digest)
	fmt.Fprintf(stdout, "Next: s2sbench okfgen --input %s --provider <provider> --model <model>\n", output)
	return nil
}

func readDuckDBCatalog(ctx context.Context, databasePath, databaseName, schemaName, specification, generatorVersion string, generatedAt time.Time) (readiness.SourceCatalog, error) {
	db, err := openDuckDB(databasePath)
	if err != nil {
		return readiness.SourceCatalog{}, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT table_name, column_name, ordinal_position, data_type, is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_catalog = current_database() AND table_schema = ?
ORDER BY table_name, ordinal_position`, schemaName)
	if err != nil {
		return readiness.SourceCatalog{}, fmt.Errorf("read DuckDB source catalog: %w", err)
	}
	defer rows.Close()
	byName := map[string]*readiness.CatalogTable{}
	var order []string
	for rows.Next() {
		var tableName, columnName, dataType, nullable, defaultValue string
		var position int
		if err := rows.Scan(&tableName, &columnName, &position, &dataType, &nullable, &defaultValue); err != nil {
			return readiness.SourceCatalog{}, err
		}
		table := byName[tableName]
		if table == nil {
			table = &readiness.CatalogTable{Name: tableName}
			byName[tableName] = table
			order = append(order, tableName)
		}
		table.Columns = append(table.Columns, readiness.CatalogColumn{Position: position, Name: columnName, Type: dataType, Nullable: nullable == "YES", Default: defaultValue})
	}
	if err := rows.Err(); err != nil {
		return readiness.SourceCatalog{}, err
	}
	catalog := readiness.SourceCatalog{SchemaVersion: readiness.CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB", Database: databaseName, Schema: schemaName, Specification: specification, Generator: "DuckDB tpcds dsdgen " + generatorVersion, GeneratedAt: generatedAt.Format(time.RFC3339), Tags: []string{"tpc-ds"}}
	for _, name := range order {
		table := byName[name]
		statement := fmt.Sprintf("SELECT count(*) FROM %s.%s", quoteIdentifier(schemaName), quoteIdentifier(name))
		if err := db.QueryRowContext(ctx, statement).Scan(&table.RowCount); err != nil {
			return readiness.SourceCatalog{}, fmt.Errorf("count source table %q: %w", name, err)
		}
		catalog.Tables = append(catalog.Tables, *table)
	}
	if err := catalog.Validate(); err != nil {
		return readiness.SourceCatalog{}, err
	}
	return catalog, nil
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func loadTPCDSModel(local string) ([]byte, string, string, error) {
	if path := strings.TrimSpace(local); path != "" {
		body, err := os.ReadFile(path)
		return body, path, "local", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(ossieTPCDSURL)
	if err != nil {
		return nil, "", "", fmt.Errorf("download pinned Apache Ossie TPC-DS model: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("download pinned Apache Ossie TPC-DS model: HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, "", "", err
	}
	return body, ossieTPCDSURL, ossieTPCDSRevision, nil
}

var (
	simpleIdentifierPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	qualifiedIdentifierPattern = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\b`)
	quotedSQLStringPattern     = regexp.MustCompile(`'([^']|'')*'`)
	sqlIdentifierPattern       = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

func scopeTPCDSCatalogToOssieExample(template []byte, physical readiness.SourceCatalog) (readiness.SourceCatalog, error) {
	if err := physical.Validate(); err != nil {
		return readiness.SourceCatalog{}, err
	}
	document, err := ossie.NewLoader().Load(template)
	if err != nil {
		return readiness.SourceCatalog{}, fmt.Errorf("validate Apache Ossie template: %w", err)
	}
	tables := make(map[string]readiness.CatalogTable, len(physical.Tables))
	for _, table := range physical.Tables {
		tables[strings.ToLower(table.Name)] = table
	}
	result := physical
	result.Tables = nil
	seen := map[string]struct{}{}
	for _, semanticModel := range document.SemanticModel {
		if semanticModel.Name != "tpcds_retail_model" {
			continue
		}
		for _, dataset := range semanticModel.Datasets {
			parts := strings.Split(dataset.Source, ".")
			if len(parts) != 3 || !strings.EqualFold(parts[0], physical.Database) || !strings.EqualFold(parts[1], physical.Schema) {
				return readiness.SourceCatalog{}, fmt.Errorf("dataset %q source %q does not target %s.%s", dataset.Name, dataset.Source, physical.Database, physical.Schema)
			}
			name := strings.ToLower(parts[2])
			if _, duplicate := seen[name]; duplicate {
				return readiness.SourceCatalog{}, fmt.Errorf("Apache Ossie example repeats source table %q", parts[2])
			}
			table, ok := tables[name]
			if !ok {
				return readiness.SourceCatalog{}, fmt.Errorf("Apache Ossie example source table %q is absent", parts[2])
			}
			seen[name] = struct{}{}
			result.Tables = append(result.Tables, table)
		}
		if len(result.Tables) != 5 {
			return readiness.SourceCatalog{}, fmt.Errorf("Apache Ossie TPC-DS example resolves to %d tables, want 5", len(result.Tables))
		}
		return result, result.Validate()
	}
	return readiness.SourceCatalog{}, fmt.Errorf("semantic model %q is absent", "tpcds_retail_model")
}

// buildTPCDSOssieModel uses the concrete DuckDB catalog as the dataset/field
// authority and Apache Ossie's pinned TPC-DS example as a semantic enrichment
// template. This avoids both inventing semantics from a physical schema and
// publishing template fields that do not exist in the generated database.
func buildTPCDSOssieModel(template []byte, catalog readiness.SourceCatalog) ([]byte, error) {
	document, err := ossie.NewLoader().Load(template)
	if err != nil {
		return nil, fmt.Errorf("validate Apache Ossie template: %w", err)
	}
	var semanticModel *ossie.SemanticModel
	for index := range document.SemanticModel {
		if document.SemanticModel[index].Name == "tpcds_retail_model" {
			semanticModel = &document.SemanticModel[index]
			break
		}
	}
	if semanticModel == nil {
		return nil, fmt.Errorf("semantic model %q is absent", "tpcds_retail_model")
	}
	templateDatasets := make(map[string]ossie.Dataset, len(semanticModel.Datasets))
	for _, dataset := range semanticModel.Datasets {
		templateDatasets[strings.ToLower(dataset.Name)] = dataset
	}
	tables := append([]readiness.CatalogTable(nil), catalog.Tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	generatedDatasets := make([]ossie.Dataset, 0, len(tables))
	for _, table := range tables {
		dataset, enriched := templateDatasets[strings.ToLower(table.Name)]
		if !enriched {
			dataset = ossie.Dataset{Name: table.Name}
		}
		dataset.Name = table.Name
		dataset.Source = catalog.Database + "." + catalog.Schema + "." + table.Name
		fieldsByName := make(map[string]ossie.Field, len(dataset.Fields))
		var computed []ossie.Field
		for _, field := range dataset.Fields {
			if catalogTableHasColumn(table, field.Name) {
				fieldsByName[strings.ToLower(field.Name)] = field
			} else if fieldExpressionsBind(table, field.Expression) {
				computed = append(computed, field)
			}
		}
		dataset.Fields = make([]ossie.Field, 0, len(table.Columns)+len(computed))
		for _, column := range table.Columns {
			field, ok := fieldsByName[strings.ToLower(column.Name)]
			if !ok || !fieldExpressionsBind(table, field.Expression) {
				field = ossie.Field{Name: column.Name, Datatype: ossieDataType(column.Type), Expression: directColumnExpression(column.Name)}
			}
			dataset.Fields = append(dataset.Fields, field)
		}
		dataset.Fields = append(dataset.Fields, computed...)
		generatedDatasets = append(generatedDatasets, dataset)
	}
	semanticModel.Datasets = generatedDatasets
	body, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode generated Ossie model: %w", err)
	}
	header := fmt.Sprintf("# Generated from the DuckDB %s catalog by S2SBench.\n# Semantic enrichments are derived from Apache Ossie %s at %s.\n", tPCDSSpecification, ossieTPCDSRevision, ossieTPCDSURL)
	body = append([]byte(header), body...)
	if err := validateTPCDSOssieBindings(body, catalog); err != nil {
		return nil, err
	}
	return body, nil
}

func directColumnExpression(column string) ossie.Expression {
	return ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: column}}}
}

func ossieDataType(physicalType string) ossie.DataType {
	value := strings.ToUpper(strings.TrimSpace(physicalType))
	switch {
	case strings.Contains(value, "CHAR"), strings.Contains(value, "TEXT"):
		return ossie.DataTypeString
	case strings.Contains(value, "INT"):
		return ossie.DataTypeInteger
	case strings.Contains(value, "DECIMAL"), strings.Contains(value, "NUMERIC"):
		return ossie.DataTypeDecimal
	case strings.Contains(value, "DOUBLE"), strings.Contains(value, "FLOAT"), strings.Contains(value, "REAL"):
		return ossie.DataTypeFloat
	case value == "BOOLEAN":
		return ossie.DataTypeBoolean
	case value == "DATE":
		return ossie.DataTypeDate
	case strings.Contains(value, "TIMESTAMP"):
		return ossie.DataTypeDateTime
	case value == "TIME":
		return ossie.DataTypeTime
	default:
		return ossie.DataTypeOpaque
	}
}

func fieldExpressionsBind(table readiness.CatalogTable, expression ossie.Expression) bool {
	if len(expression.Dialects) == 0 {
		return false
	}
	for _, dialect := range expression.Dialects {
		value := strings.TrimSpace(dialect.Expression)
		if simpleIdentifierPattern.MatchString(value) {
			if !catalogTableHasColumn(table, value) {
				return false
			}
			continue
		}
		// Computed fields in the pinned Ossie template are simple ANSI
		// expressions. Ignore string literals and require every remaining
		// identifier to be a physical column.
		withoutStrings := quotedSQLStringPattern.ReplaceAllString(value, " ")
		for _, token := range sqlIdentifierPattern.FindAllString(withoutStrings, -1) {
			if !catalogTableHasColumn(table, token) {
				return false
			}
		}
	}
	return true
}

// validateTPCDSOssieBindings keeps the two generated interfaces honest. The
// physical catalog remains the OKF authority, while Apache Ossie's TPC-DS
// example remains the semantic authority used by Metis. Every semantic source,
// key, direct field expression, relationship, and qualified metric reference
// must resolve against the concrete DuckDB schema generated in this run.
func validateTPCDSOssieBindings(model []byte, catalog readiness.SourceCatalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	document, err := ossie.NewLoader().Load(model)
	if err != nil {
		return fmt.Errorf("validate Apache Ossie document: %w", err)
	}
	tables := make(map[string]readiness.CatalogTable, len(catalog.Tables))
	for _, table := range catalog.Tables {
		tables[strings.ToLower(table.Name)] = table
	}
	for _, semanticModel := range document.SemanticModel {
		if semanticModel.Name != "tpcds_retail_model" {
			continue
		}
		datasets := make(map[string]readiness.CatalogTable, len(semanticModel.Datasets))
		for _, dataset := range semanticModel.Datasets {
			parts := strings.Split(dataset.Source, ".")
			if len(parts) != 3 || !strings.EqualFold(parts[0], catalog.Database) || !strings.EqualFold(parts[1], catalog.Schema) {
				return fmt.Errorf("dataset %q source %q does not target %s.%s", dataset.Name, dataset.Source, catalog.Database, catalog.Schema)
			}
			table, ok := tables[strings.ToLower(parts[2])]
			if !ok {
				return fmt.Errorf("dataset %q source table %q is absent", dataset.Name, parts[2])
			}
			datasets[strings.ToLower(dataset.Name)] = table
			for _, column := range append(append([]string(nil), dataset.PrimaryKey...), flattenKeys(dataset.UniqueKeys)...) {
				if !catalogTableHasColumn(table, column) {
					return fmt.Errorf("dataset %q key column %q is absent from table %q", dataset.Name, column, table.Name)
				}
			}
			for _, field := range dataset.Fields {
				if !fieldExpressionsBind(table, field.Expression) {
					return fmt.Errorf("dataset %q field %q does not bind to table %q", dataset.Name, field.Name, table.Name)
				}
			}
		}
		for _, relationship := range semanticModel.Relationships {
			from, ok := datasets[strings.ToLower(relationship.From)]
			if !ok {
				return fmt.Errorf("relationship %q references absent dataset %q", relationship.Name, relationship.From)
			}
			to, ok := datasets[strings.ToLower(relationship.To)]
			if !ok {
				return fmt.Errorf("relationship %q references absent dataset %q", relationship.Name, relationship.To)
			}
			for _, column := range relationship.FromColumns {
				if !catalogTableHasColumn(from, column) {
					return fmt.Errorf("relationship %q references absent column %s.%s", relationship.Name, relationship.From, column)
				}
			}
			for _, column := range relationship.ToColumns {
				if !catalogTableHasColumn(to, column) {
					return fmt.Errorf("relationship %q references absent column %s.%s", relationship.Name, relationship.To, column)
				}
			}
		}
		for _, metric := range semanticModel.Metrics {
			for _, dialect := range metric.Expression.Dialects {
				for _, match := range qualifiedIdentifierPattern.FindAllStringSubmatch(dialect.Expression, -1) {
					table, ok := datasets[strings.ToLower(match[1])]
					if !ok || !catalogTableHasColumn(table, match[2]) {
						return fmt.Errorf("metric %q references absent semantic column %s.%s", metric.Name, match[1], match[2])
					}
				}
			}
		}
		return nil
	}
	return fmt.Errorf("semantic model %q is absent", "tpcds_retail_model")
}

func flattenKeys(keys [][]string) []string {
	var flattened []string
	for _, key := range keys {
		flattened = append(flattened, key...)
	}
	return flattened
}

func catalogTableHasColumn(table readiness.CatalogTable, name string) bool {
	for _, column := range table.Columns {
		if strings.EqualFold(column.Name, name) {
			return true
		}
	}
	return false
}

func openDuckDB(path string) (*sql.DB, error) {
	connector, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func generateStandardTPCDS(ctx context.Context, path string, scale float64) (string, []string, error) {
	db, err := openDuckDB(path)
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "LOAD tpcds"); err != nil {
		loadErr := err
		if _, installErr := db.ExecContext(ctx, "INSTALL tpcds"); installErr != nil {
			return "", nil, fmt.Errorf("load DuckDB tpcds extension: %v; install missing extension: %w", loadErr, installErr)
		}
		if _, err := db.ExecContext(ctx, "LOAD tpcds"); err != nil {
			return "", nil, fmt.Errorf("load installed DuckDB tpcds extension: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA public"); err != nil {
		return "", nil, fmt.Errorf("create Ossie TPC-DS schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CALL dsdgen(sf = ?, schema = 'public')", scale); err != nil {
		return "", nil, fmt.Errorf("generate TPC-DS data with dsdgen: %w", err)
	}
	var duckdbVersion, extensionVersion string
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&duckdbVersion); err != nil {
		return "", nil, err
	}
	if err := db.QueryRowContext(ctx, "SELECT extension_version FROM duckdb_extensions() WHERE extension_name = 'tpcds'").Scan(&extensionVersion); err != nil {
		return "", nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT table_name FROM duckdb_tables() WHERE database_name = current_database() AND schema_name = 'public' ORDER BY table_name")
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return "", nil, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if len(tables) != 24 {
		return "", nil, fmt.Errorf("dsdgen created %d tables, want the 24-table TPC-DS schema", len(tables))
	}
	return fmt.Sprintf("tpcds-kit=2.10.0;duckdb=%s;extension=%s", duckdbVersion, extensionVersion), tables, nil
}

func exportTPCDSData(ctx context.Context, root, database string, tables []string) ([]workload.Asset, error) {
	db, err := openDuckDB(database)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	assets := make([]workload.Asset, 0, len(tables)*2)
	for _, format := range []string{"csv", "parquet"} {
		for _, table := range tables {
			relative := filepath.ToSlash(filepath.Join("data", format, table+"."+format))
			output := filepath.Join(root, filepath.FromSlash(relative))
			options := "FORMAT PARQUET"
			if format == "csv" {
				options = "FORMAT CSV, HEADER"
			}
			statement := fmt.Sprintf("COPY public.%s TO %s (%s)", table, sqlString(output), options)
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return nil, fmt.Errorf("export %s as %s: %w", table, format, err)
			}
			digest, err := workload.FileDigest(output)
			if err != nil {
				return nil, err
			}
			assets = append(assets, workload.Asset{Path: relative, SHA256: digest, Format: format})
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	return assets, nil
}

func writeTPCDSSchema(ctx context.Context, database, output string) error {
	db, err := openDuckDB(database)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT sql FROM duckdb_tables() WHERE database_name = current_database() AND schema_name = 'public' ORDER BY table_name")
	if err != nil {
		return err
	}
	defer rows.Close()
	var out strings.Builder
	out.WriteString("-- Generated from the standard dsdgen schema; informational and not used as oracle authority.\n")
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			return err
		}
		out.WriteString(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
		out.WriteString(";\n")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return os.WriteFile(output, []byte(out.String()), 0o644)
}

func sqlString(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func tpcdsLoaderSQL(tables []string) string {
	var out strings.Builder
	out.WriteString("-- Run from the generated bundle's data directory. Recreates the full standard TPC-DS schema from Parquet.\nATTACH 'tpcds.duckdb' AS tpcds;\nCREATE SCHEMA IF NOT EXISTS tpcds.public;\n")
	for _, table := range tables {
		fmt.Fprintf(&out, "CREATE OR REPLACE TABLE tpcds.public.%s AS SELECT * FROM read_parquet('parquet/%s.parquet');\n", table, table)
	}
	return out.String()
}

type tpcdsCaseDefinition struct {
	name, stratum, question, referenceSQL string
	query                                 query.SemanticQuery
}

func tpcdsCaseDefinitions() []tpcdsCaseDefinition {
	limit := 100
	return []tpcdsCaseDefinition{
		{"total_sales", "control", "What is total sales revenue overall?", "SELECT SUM(ss_ext_sales_price) AS total_sales FROM tpcds.public.store_sales", query.SemanticQuery{Project: "tpcds", Model: "tpcds_retail_model", Metrics: []query.MetricRef{{Name: "total_sales"}}}},
		{"sales_and_profit", "control", "What are total sales and total profit overall?", "SELECT SUM(ss_ext_sales_price) AS total_sales, SUM(ss_net_profit) AS total_profit FROM tpcds.public.store_sales", query.SemanticQuery{Project: "tpcds", Model: "tpcds_retail_model", Metrics: []query.MetricRef{{Name: "total_sales"}, {Name: "total_profit"}}}},
		{"sales_by_year", "fanout", "What is total sales revenue for each year?", "SELECT d.d_year, SUM(ss.ss_ext_sales_price) AS total_sales FROM tpcds.public.store_sales ss JOIN tpcds.public.date_dim d ON ss.ss_sold_date_sk = d.d_date_sk GROUP BY d.d_year ORDER BY d.d_year LIMIT 100", query.SemanticQuery{Project: "tpcds", Model: "tpcds_retail_model", Metrics: []query.MetricRef{{Name: "total_sales"}}, Dimensions: []query.DimensionRef{{Name: "d_year"}}, OrderBy: []query.OrderBy{{Field: "d_year", Direction: query.SortAsc}}, Limit: &limit}},
		{"sales_by_brand", "fanout", "For each product brand, what is total sales revenue? Order the rows alphabetically by brand and return the first 100 rows.", "SELECT i.i_brand, SUM(ss.ss_ext_sales_price) AS total_sales FROM tpcds.public.store_sales ss JOIN tpcds.public.item i ON ss.ss_item_sk = i.i_item_sk GROUP BY i.i_brand ORDER BY i.i_brand LIMIT 100", query.SemanticQuery{Project: "tpcds", Model: "tpcds_retail_model", Metrics: []query.MetricRef{{Name: "total_sales"}}, Dimensions: []query.DimensionRef{{Name: "i_brand"}}, OrderBy: []query.OrderBy{{Field: "i_brand", Direction: query.SortAsc}}, Limit: &limit}},
		{"customer_lifetime_value", "silent_semantics", "What is customer lifetime value for the first 100 customer IDs?", "SELECT c.c_customer_id, SUM(ss.ss_ext_sales_price) / COUNT(DISTINCT c.c_customer_sk) AS customer_lifetime_value FROM tpcds.public.store_sales ss JOIN tpcds.public.customer c ON ss.ss_customer_sk = c.c_customer_sk GROUP BY c.c_customer_id ORDER BY c.c_customer_id LIMIT 100", query.SemanticQuery{Project: "tpcds", Model: "tpcds_retail_model", Metrics: []query.MetricRef{{Name: "customer_lifetime_value"}}, Dimensions: []query.DimensionRef{{Name: "c_customer_id"}}, OrderBy: []query.OrderBy{{Field: "c_customer_id", Direction: query.SortAsc}}, Limit: &limit}},
		{"california_sales", "edge", "What is total sales revenue for stores in California?", "SELECT SUM(ss.ss_ext_sales_price) AS total_sales FROM tpcds.public.store_sales ss JOIN tpcds.public.store s ON ss.ss_store_sk = s.s_store_sk WHERE s.s_state = 'CA'", query.SemanticQuery{Project: "tpcds", Model: "tpcds_retail_model", Metrics: []query.MetricRef{{Name: "total_sales"}}, Filters: []query.Filter{{Field: "s_state", Operator: query.FilterEQ, Value: "CA"}}}},
	}
}

func freezeTPCDSOracles(ctx context.Context, runtimeRoot, bundleRoot, database string, semanticDefinition fixtures.Definition) ([]workload.Case, error) {
	projectPath, project, err := writeSemanticProject(runtimeRoot, []fixtures.Definition{semanticDefinition})
	if err != nil {
		return nil, err
	}
	runtime, err := loadSemanticProjectRuntime(projectPath, project, database)
	if err != nil {
		return nil, err
	}
	defer runtime.Execution.Close(context.Background())
	backend, err := duckdbfixture.New(database)
	if err != nil {
		return nil, err
	}
	defer backend.Close(context.Background())
	cases := make([]workload.Case, 0, len(tpcdsCaseDefinitions()))
	for _, definition := range tpcdsCaseDefinitions() {
		referenceRelative := filepath.ToSlash(filepath.Join("cases", definition.name, "reference.sql"))
		referencePath := filepath.Join(bundleRoot, filepath.FromSlash(referenceRelative))
		if err := os.MkdirAll(filepath.Dir(referencePath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(referencePath, []byte(definition.referenceSQL+";\n"), 0o644); err != nil {
			return nil, err
		}
		referenceResult, err := backend.RunSQL(ctx, definition.referenceSQL)
		if err != nil {
			return nil, fmt.Errorf("execute provider reference SQL for %q: %w", definition.name, err)
		}
		canonicalizeResultRows(referenceResult.Rows)
		oracle := scenarios.ResultExpectation{ResultSet: referenceResult, Comparison: scenarios.ResultUnordered}
		compiled, err := runtime.Compile.Compile(ctx, service.CompileRequest{Query: definition.query, Dialect: "DUCKDB"})
		if err != nil {
			return nil, fmt.Errorf("compile generated case %q: %w", definition.name, err)
		}
		metisResult, err := backend.RunSQL(ctx, compiled.SQLRenderResult.SQL, compiled.SQLRenderResult.Parameters...)
		if err != nil {
			return nil, fmt.Errorf("cross-validate Metis SQL for %q: %w", definition.name, err)
		}
		verdict, comparisonErr := s2sbench.Judge(scenarios.Scenario{Name: definition.name, ExpectedResult: &oracle}, metisResult)
		if verdict != s2sbench.VerdictCorrect || comparisonErr != nil {
			return nil, fmt.Errorf("cross-validate Metis SQL for %q: verdict=%s: %w", definition.name, verdict, comparisonErr)
		}
		digest, err := workload.FileDigest(referencePath)
		if err != nil {
			return nil, err
		}
		cases = append(cases, workload.Case{
			Name: definition.name, Stratum: definition.stratum, Question: definition.question, Query: definition.query,
			ReferenceSQL: workload.Asset{Path: referenceRelative, SHA256: digest, Format: "sql"}, Oracle: oracle,
			OracleAuthority: "provider-reference-sql-on-dsdgen-data", CrossValidation: "metis-match",
		})
	}
	return cases, nil
}

func canonicalizeResultRows(rows []scenarios.ResultRow) {
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		for column := 0; column < len(left) && column < len(right); column++ {
			leftKey := fmt.Sprintf("%s\x00%t\x00%s", left[column].ValueKind, left[column].Null, left[column].Canonical)
			rightKey := fmt.Sprintf("%s\x00%t\x00%s", right[column].ValueKind, right[column].Null, right[column].Canonical)
			if leftKey != rightKey {
				return leftKey < rightKey
			}
		}
		return len(left) < len(right)
	})
}

func jsonEncoder(writer io.Writer) interface{ Encode(any) error } {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder
}
