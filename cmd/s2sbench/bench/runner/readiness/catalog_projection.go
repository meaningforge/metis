package readiness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const CatalogSchemaVersion = "s2sbench-source-catalog-v1"

// SourceCatalog is engine-neutral source metadata captured by a workload
// provider. It mirrors the source/list_concepts/read_concept boundary used by
// Google's OKF reference agent without making S2SBench depend on an LLM or a
// warehouse-specific catalog SDK.
type SourceCatalog struct {
	SchemaVersion string         `json:"schema_version"`
	Engine        string         `json:"engine"`
	EngineTitle   string         `json:"engine_title"`
	Database      string         `json:"database"`
	Schema        string         `json:"schema"`
	Specification string         `json:"specification,omitempty"`
	Generator     string         `json:"generator"`
	GeneratedAt   string         `json:"generated_at"`
	Tags          []string       `json:"tags,omitempty"`
	Tables        []CatalogTable `json:"tables"`
}

type CatalogTable struct {
	Name     string          `json:"name"`
	RowCount int64           `json:"row_count"`
	Columns  []CatalogColumn `json:"columns"`
}

type CatalogColumn struct {
	Position int    `json:"position"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default,omitempty"`
}

// ReferenceAgentConceptDocument is the payload returned by the upstream
// write_concept_doc skill. Source identity and catalog facts remain owned by
// the deterministic projection; the agent may enrich only editorial metadata
// and body prose.
type ReferenceAgentConceptDocument struct {
	Frontmatter map[string]any
	Body        string
}

func (c SourceCatalog) Validate() error {
	if c.SchemaVersion != CatalogSchemaVersion || strings.TrimSpace(c.Engine) == "" || strings.TrimSpace(c.EngineTitle) == "" || strings.TrimSpace(c.Database) == "" || strings.TrimSpace(c.Schema) == "" || strings.TrimSpace(c.Generator) == "" {
		return fmt.Errorf("source catalog identity is incomplete")
	}
	if _, err := time.Parse(time.RFC3339, c.GeneratedAt); err != nil {
		return fmt.Errorf("source catalog generated_at must be RFC3339: %w", err)
	}
	if len(c.Tables) == 0 {
		return fmt.Errorf("source catalog contains no tables")
	}
	seenTables := map[string]struct{}{}
	for _, table := range c.Tables {
		if strings.TrimSpace(table.Name) == "" || table.RowCount < 0 || len(table.Columns) == 0 {
			return fmt.Errorf("source catalog contains an incomplete table")
		}
		if _, duplicate := seenTables[table.Name]; duplicate {
			return fmt.Errorf("source catalog repeats table %q", table.Name)
		}
		seenTables[table.Name] = struct{}{}
		seenColumns := map[string]struct{}{}
		for index, column := range table.Columns {
			if column.Position != index+1 || strings.TrimSpace(column.Name) == "" || strings.TrimSpace(column.Type) == "" {
				return fmt.Errorf("source catalog table %q has an incomplete or unordered column", table.Name)
			}
			if _, duplicate := seenColumns[column.Name]; duplicate {
				return fmt.Errorf("source catalog table %q repeats column %q", table.Name, column.Name)
			}
			seenColumns[column.Name] = struct{}{}
		}
	}
	return nil
}

func DecodeSourceCatalog(body []byte) (SourceCatalog, error) {
	var catalog SourceCatalog
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return SourceCatalog{}, fmt.Errorf("decode source catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return SourceCatalog{}, err
	}
	return catalog, nil
}

// BuildCatalogProjection produces a deterministic OKF v0.2 bundle from source
// catalog metadata. Ossie is deliberately not an input to this projection.
func BuildCatalogProjection(catalog SourceCatalog) (*Projection, error) {
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	tables := append([]CatalogTable(nil), catalog.Tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	files := map[string][]byte{}
	var facts []Fact
	files["datasets/"+safeName(catalog.Database)+".md"] = renderCatalogDataset(catalog, tables)
	for _, table := range tables {
		path := "tables/" + safeName(table.Name) + ".md"
		files[path] = renderCatalogTable(catalog, table)
		tableJSON, _ := json.Marshal(table)
		facts = append(facts, newCatalogFact("catalog.json#/tables/"+table.Name, "/tables/"+table.Name, path, string(tableJSON)))
	}
	files["index.md"] = renderCatalogRootIndex(catalog, len(tables))
	files["datasets/index.md"] = renderCatalogDatasetIndex(catalog)
	files["tables/index.md"] = renderCatalogTableIndex(catalog, tables)
	sort.Slice(facts, func(i, j int) bool { return facts[i].CanonicalPath < facts[j].CanonicalPath })
	ledger := Ledger{SchemaVersion: "okf-source-fact-ledger-v1", ProjectionVersion: CatalogProjectionVersion, OKFRevision: OKFRevision, Facts: facts}
	ledger.CanonicalDigest = canonicalFactDigest(facts)
	ledger.Digest = digestJSON(facts)
	projection := &Projection{Project: catalog.Database, Files: files, Ledger: ledger}
	if err := projection.Validate(); err != nil {
		return nil, err
	}
	return projection, nil
}

// AddReferenceAgentConceptBodies applies the body supplied through the Google
// OKF reference-agent write_concept_doc contract. The deterministic catalog
// document is read first and retained as the factual base; the agent's single
// concept write becomes a clearly separated editorial section. This preserves
// the catalog fact ledger as physical-source evidence rather than letting LLM
// output become semantic or oracle authority.
func AddReferenceAgentConceptBodies(projection *Projection, bodies map[string]string) error {
	if projection == nil {
		return fmt.Errorf("catalog projection is required")
	}
	for path, body := range bodies {
		body = strings.TrimSpace(body)
		if !strings.HasPrefix(path, "datasets/") && !strings.HasPrefix(path, "tables/") {
			return fmt.Errorf("reference-agent body path %q is not a source concept", path)
		}
		if _, ok := projection.Files[path]; !ok {
			return fmt.Errorf("reference-agent body targets unknown concept %q", path)
		}
		if body == "" || len(body) > 24<<10 || strings.HasPrefix(body, "---") {
			return fmt.Errorf("reference-agent body for %q must be Markdown without frontmatter", path)
		}
		projection.Files[path] = append(projection.Files[path], []byte("\n# Reference-agent guidance\n\nThis section is a frozen explanatory projection produced through the Google OKF reference-agent workflow. Physical catalog facts retain the catalog as their authority; semantic definitions retain the generated Ossie model as their authority; neither changes the benchmark oracle.\n\n"+body+"\n")...)
	}
	return projection.Validate()
}

// ApplyReferenceAgentConceptDocuments applies the write_concept_doc payloads
// after preserving catalog-owned frontmatter. In particular `type`, `resource`,
// `generated`, and `sources` continue to identify the frozen source catalog;
// an agent cannot replace them with invented authority.
func ApplyReferenceAgentConceptDocuments(projection *Projection, documents map[string]ReferenceAgentConceptDocument) error {
	if projection == nil {
		return fmt.Errorf("catalog projection is required")
	}
	bodies := make(map[string]string, len(documents))
	for path, document := range documents {
		body := projection.Files[path]
		if len(body) == 0 {
			return fmt.Errorf("reference-agent document targets unknown concept %q", path)
		}
		base, err := decodeFrontmatter(body)
		if err != nil {
			return fmt.Errorf("read catalog frontmatter for %q: %w", path, err)
		}
		if strings.TrimSpace(fmt.Sprint(document.Frontmatter["type"])) == "" || fmt.Sprint(document.Frontmatter["type"]) != fmt.Sprint(base["type"]) {
			return fmt.Errorf("reference-agent document %q changes required type", path)
		}
		for _, key := range []string{"title", "description", "tags", "status"} {
			if value, ok := document.Frontmatter[key]; ok {
				base[key] = value
			}
		}
		encoded, err := yaml.Marshal(base)
		if err != nil {
			return fmt.Errorf("encode reference-agent frontmatter for %q: %w", path, err)
		}
		end := bytes.Index(body[4:], []byte("\n---\n"))
		if end < 0 {
			return fmt.Errorf("catalog frontmatter for %q is unterminated", path)
		}
		projection.Files[path] = append(append([]byte("---\n"), encoded...), append([]byte("---\n"), body[4+end+5:]...)...)
		bodies[path] = document.Body
	}
	return AddReferenceAgentConceptBodies(projection, bodies)
}

func newCatalogFact(canonicalPath, sourcePath, okfDocument, value string) Fact {
	return Fact{
		CanonicalPath: canonicalPath, SourceDocument: "catalog.json", SourcePath: sourcePath,
		OKFDocument: okfDocument, OKFPath: "Schema", CanonicalJSON: value,
		Digest: digestString(canonicalPath + "\x00" + value), Semantic: false,
	}
}

func renderCatalogFrontmatter(catalog SourceCatalog, kind, title, description, resource string, tags []string) string {
	return fmt.Sprintf("---\ntype: %s\ntitle: %s\ndescription: %s\nresource: %s\ntags: %s\ngenerated: { by: process:s2sbench-catalog-to-okf-v1, at: %s }\nstatus: stable\nsources:\n  - { id: source-catalog, resource: %s, title: %s, author: %s }\n---\n",
		yamlString(kind), yamlString(title), yamlString(description), yamlString(resource), yamlStringList(tags), yamlString(catalog.GeneratedAt),
		yamlString(catalog.Engine+"://"+catalog.Database+"/"+catalog.Schema), yamlString(catalog.Generator+" source catalog"), yamlString("process:"+safeName(catalog.Generator)),
	)
}

func renderCatalogDataset(catalog SourceCatalog, tables []CatalogTable) []byte {
	subject := firstNonEmpty(catalog.Specification, catalog.Database+"."+catalog.Schema)
	description := fmt.Sprintf("%s dataset containing %d physical tables generated by %s.", subject, len(tables), catalog.Generator)
	var out strings.Builder
	tags := append([]string{"dataset", catalog.Engine}, catalog.Tags...)
	out.WriteString(renderCatalogFrontmatter(catalog, catalogType(catalog.EngineTitle, "Dataset"), catalog.Database+"."+catalog.Schema, description, catalog.Engine+"://"+catalog.Database+"/"+catalog.Schema, tags))
	out.WriteString("\n# Tables\n\n")
	for _, table := range tables {
		fmt.Fprintf(&out, "- [%s](../tables/%s.md) — %d columns, %d rows.\n", table.Name, safeName(table.Name), len(table.Columns), table.RowCount)
	}
	out.WriteString("\n# Provenance\n\nThe table and column metadata in this catalog-derived layer was read from the generated database catalog. Semantic material, when later added by the reference-agent workflow, is separately sourced from the generated Ossie model.[^source-catalog]\n\n[^source-catalog]: Generated DuckDB source catalog.\n")
	return []byte(out.String())
}

func renderCatalogTable(catalog SourceCatalog, table CatalogTable) []byte {
	qualified := catalog.Schema + "." + table.Name
	description := fmt.Sprintf("Physical %s table with %d columns and %d rows.", qualified, len(table.Columns), table.RowCount)
	var out strings.Builder
	tags := append([]string{"table", table.Name}, catalog.Tags...)
	out.WriteString(renderCatalogFrontmatter(catalog, catalogType(catalog.EngineTitle, "Table"), qualified, description, catalog.Engine+"://"+catalog.Database+"/"+catalog.Schema+"/"+table.Name, tags))
	out.WriteString("\n# Schema\n\n| Column | Type | Nullable | Default |\n| --- | --- | --- | --- |\n")
	for _, column := range table.Columns {
		nullable := "no"
		if column.Nullable {
			nullable = "yes"
		}
		fallback := column.Default
		if fallback == "" {
			fallback = "—"
		}
		fmt.Fprintf(&out, "| `%s` | `%s` | %s | %s |\n", escapeMarkdownTable(column.Name), escapeMarkdownTable(column.Type), nullable, escapeMarkdownTable(fallback))
	}
	fmt.Fprintf(&out, "\n# Physical profile\n\n- Row count at generation: `%d`.\n- Qualified name: `%s`.\n- Database engine: `%s`.\n", table.RowCount, qualified, catalog.Engine)
	out.WriteString("\n# Related concepts\n\n- [TPC-DS dataset](../datasets/" + safeName(catalog.Database) + ".md)\n")
	return []byte(out.String())
}

func renderCatalogRootIndex(catalog SourceCatalog, tableCount int) []byte {
	return []byte(fmt.Sprintf("---\nokf_version: \"%s\"\n---\n\n# %s source catalog\n\n- [Datasets](datasets/index.md) — 1 dataset.\n- [Tables](tables/index.md) — %d physical tables.\n", OKFVersion, catalog.Specification, tableCount))
}

func renderCatalogDatasetIndex(catalog SourceCatalog) []byte {
	return []byte(fmt.Sprintf("# %s\n\n* [%s.%s](%s.md) - %s physical dataset.\n", catalogType(catalog.EngineTitle, "Dataset"), catalog.Database, catalog.Schema, safeName(catalog.Database), firstNonEmpty(catalog.Specification, catalog.EngineTitle)))
}

func renderCatalogTableIndex(catalog SourceCatalog, tables []CatalogTable) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\n", catalogType(catalog.EngineTitle, "Table"))
	for _, table := range tables {
		fmt.Fprintf(&out, "* [%s.%s](%s.md) - %d columns, %d rows.\n", catalog.Schema, table.Name, safeName(table.Name), len(table.Columns), table.RowCount)
	}
	return []byte(out.String())
}

func catalogType(engineTitle, asset string) string {
	engineTitle = strings.TrimSpace(engineTitle)
	if engineTitle == "" {
		return asset
	}
	return engineTitle + " " + asset
}
