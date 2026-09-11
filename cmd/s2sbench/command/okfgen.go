package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	okfgenskill "github.com/meaningforge/metis/cmd/s2sbench/bench/agent/okfgen"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
	"github.com/spf13/cobra"
)

const (
	googleOKFReferenceAgentRevision = "ad30107c31c06aec8a7d5636e0d1058118604e6f"
	googleOKFReferenceAgentWorkflow = "google-okf-reference-agent-skill-v1"
	maxReferenceAgentAttempts       = 2
)

var googleOKFReferenceAgentSkills = []string{
	"list_concepts",
	"read_existing_doc",
	"read_concept_raw",
	"sample_rows",
	"read_semantic_context",
	"write_concept_doc",
}

type okfgenOptions struct {
	Input     string
	Provider  string
	Model     string
	PiBin     string
	TimeoutMS int
	Skill     string
	Debug     bool
}

// referenceAgentWrite is the argument shape of Google's write_concept_doc
// tool. Pi returns it as JSON because it cannot call the Python ADK tool
// directly; the Go source adapter performs the same read/write boundary.
type referenceAgentWrite struct {
	ConceptID   string         `json:"concept_id"`
	Frontmatter map[string]any `json:"frontmatter"`
	Body        string         `json:"body"`
}

func newOKFGenCommand(stdout io.Writer) *cobra.Command {
	options := okfgenOptions{Input: ".workload", PiBin: "pi", TimeoutMS: 600_000}
	command := &cobra.Command{
		Use:   "okfgen",
		Short: "Generate and freeze OKF documents with the Google reference-agent workflow",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return generateOKF(context.Background(), options, stdout)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Input, "input", options.Input, "workload foundation created by s2sbench gen")
	flags.StringVar(&options.Provider, "provider", options.Provider, "Pi model provider")
	flags.StringVar(&options.Model, "model", options.Model, "Pi model ID")
	flags.StringVar(&options.PiBin, "pi-bin", options.PiBin, "Pi CLI binary")
	flags.IntVar(&options.TimeoutMS, "timeout-ms", options.TimeoutMS, "per-concept reference-agent timeout in milliseconds")
	flags.StringVar(&options.Skill, "skill", options.Skill, "Externally supplied Google OKF Agent Skill directory (required)")
	flags.BoolVar(&options.Debug, "debug", options.Debug, "stream Pi model and source-tool events while generating OKF")
	_ = command.MarkFlagRequired("provider")
	_ = command.MarkFlagRequired("model")
	_ = command.MarkFlagRequired("skill")
	return command
}

func generateOKF(ctx context.Context, options okfgenOptions, stdout io.Writer) error {
	inputValue := strings.TrimSpace(options.Input)
	if inputValue == "" {
		inputValue = ".workload"
	}
	root, err := filepath.Abs(inputValue)
	if err != nil {
		return fmt.Errorf("resolve workload input %q: %w", inputValue, err)
	}
	bundle, err := workload.Load(root)
	if err != nil {
		return fmt.Errorf("load workload foundation: %w", err)
	}
	if bundle.Provider != "ossie-tpcds-example" || bundle.Engine != "duckdb" {
		return fmt.Errorf("okfgen does not support workload provider=%q engine=%q", bundle.Provider, bundle.Engine)
	}
	if bundle.Knowledge.Format != "" {
		return fmt.Errorf("workload %q already has frozen OKF knowledge; generate a new foundation to rerun okfgen", root)
	}
	if strings.TrimSpace(options.Provider) == "" || strings.TrimSpace(options.Model) == "" || strings.TrimSpace(options.PiBin) == "" || options.TimeoutMS <= 0 {
		return fmt.Errorf("--provider, --model, --pi-bin, and positive --timeout-ms are required")
	}
	if strings.TrimSpace(options.Skill) == "" {
		return fmt.Errorf("--skill is required; provide a directory containing SKILL.md")
	}
	options.Skill, err = filepath.Abs(options.Skill)
	if err != nil {
		return fmt.Errorf("resolve OKF skill: %w", err)
	}
	if info, statErr := os.Stat(filepath.Join(options.Skill, "SKILL.md")); statErr != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Google OKF Agent Skill %q is unavailable", options.Skill)
	}
	if _, err := exec.Command(options.PiBin, "--version").Output(); err != nil {
		return fmt.Errorf("resolve Pi for Google OKF reference-agent workflow: %w", err)
	}
	catalogPath := filepath.Join(root, "catalog", "catalog.json")
	catalogBody, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("read source catalog: %w", err)
	}
	catalog, err := readiness.DecodeSourceCatalog(catalogBody)
	if err != nil {
		return err
	}
	semantic, err := loadOKFSemanticContext(root, bundle)
	if err != nil {
		return err
	}
	projection, err := readiness.BuildCatalogProjection(catalog)
	if err != nil {
		return err
	}
	partial := filepath.Join(root, "knowledge.partial")
	knowledgeRoot := filepath.Join(root, "knowledge")
	if _, err := os.Stat(knowledgeRoot); err == nil {
		return fmt.Errorf("knowledge directory %q already exists", knowledgeRoot)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(partial); err == nil {
		return fmt.Errorf("partial knowledge directory %q already exists", partial)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := projection.WriteBundle(partial); err != nil {
		return err
	}
	extension, cleanupExtension, err := okfgenskill.MaterializeExtension()
	if err != nil {
		return err
	}
	defer cleanupExtension()
	toolRoot, err := os.MkdirTemp("", "metis-s2sbench-okfgen-source-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(toolRoot)
	concepts := referenceAgentConcepts(catalog)
	documents := make(map[string]readiness.ReferenceAgentConceptDocument, len(concepts))
	promptParts := make([]string, 0, len(concepts))
	outputParts := make([]string, 0, len(concepts))
	for _, concept := range concepts {
		raw, err := referenceAgentRawConcept(catalog, concept)
		if err != nil {
			return err
		}
		semanticRaw, err := semantic.forConcept(concept)
		if err != nil {
			return err
		}
		var repair string
		var accepted referenceAgentWrite
		for attempt := 1; attempt <= maxReferenceAgentAttempts; attempt++ {
			contextPath, writePath, contextErr := writeReferenceAgentToolContext(toolRoot, partial, concept, concepts, raw, semanticRaw)
			if contextErr != nil {
				return contextErr
			}
			prompt, promptErr := referenceAgentPrompt(catalog, concept, repair)
			if promptErr != nil {
				return promptErr
			}
			var trace io.Writer
			if options.Debug && stdout != nil {
				trace = newPiDebugRenderer(stdout)
				fmt.Fprintf(stdout, "[okfgen debug] concept=%s attempt=%d/%d provider=%s model=%s\\n", concept.ID, attempt, maxReferenceAgentAttempts, options.Provider, options.Model)
			}
			turnCtx, cancel := context.WithTimeout(ctx, time.Duration(options.TimeoutMS)*time.Millisecond)
			response, runErr := runPiReferenceAgent(turnCtx, options.PiBin, options.Provider, options.Model, extension, contextPath, options.Skill, prompt, trace)
			cancel()
			promptParts = append(promptParts, prompt)
			outputParts = append(outputParts, response)
			if runErr != nil {
				return fmt.Errorf("Google OKF reference-agent concept %q: %w", concept.ID, runErr)
			}
			write, decodeErr := loadReferenceAgentWrite(writePath)
			if decodeErr != nil {
				repair = "The previous write_concept_doc arguments were invalid JSON: " + decodeErr.Error()
				continue
			}
			if write.ConceptID != concept.ID {
				repair = fmt.Sprintf("The previous write targeted %q; it must target exactly %q.", write.ConceptID, concept.ID)
				continue
			}
			if strings.TrimSpace(fmt.Sprint(write.Frontmatter["type"])) != concept.Type {
				repair = fmt.Sprintf("The previous type %q must be exactly %q.", write.Frontmatter["type"], concept.Type)
				continue
			}
			if validationErr := validateReferenceAgentBody(write.Body); validationErr != nil {
				repair = "The previous document failed the source-backed write validator: " + validationErr.Error()
				continue
			}
			accepted = write
			break
		}
		if accepted.ConceptID == "" {
			return fmt.Errorf("Google OKF reference-agent concept %q did not produce a source-valid write after %d attempts: %s", concept.ID, maxReferenceAgentAttempts, repair)
		}
		documents[concept.Path] = readiness.ReferenceAgentConceptDocument{Frontmatter: accepted.Frontmatter, Body: accepted.Body}
	}
	if err := readiness.ApplyReferenceAgentConceptDocuments(projection, documents); err != nil {
		return fmt.Errorf("validate Google OKF reference-agent writes: %w", err)
	}
	// The source adapter needs catalog documents on disk before Pi can read them.
	// ApplyReferenceAgentConceptDocuments updates the in-memory projection, so
	// write the accepted reference-agent guidance back before freezing it.
	if err := projection.WriteBundle(partial); err != nil {
		return fmt.Errorf("persist Google OKF reference-agent documents: %w", err)
	}
	if err := os.Rename(partial, knowledgeRoot); err != nil {
		return err
	}
	knowledge, err := frozenKnowledgeBundle(root, projection, catalogPath, promptParts, outputParts, options)
	if err != nil {
		return err
	}
	bundle.Knowledge = knowledge
	if err := bundle.Seal(); err != nil {
		return err
	}
	if err := writeWorkloadBundle(root, bundle); err != nil {
		return err
	}
	if _, err := workload.Load(root); err != nil {
		return fmt.Errorf("verify frozen OKF workload: %w", err)
	}
	fmt.Fprintf(stdout, "S2SBench OKF generated and frozen: %s\n", knowledgeRoot)
	fmt.Fprintf(stdout, "workflow=%s upstream_revision=%s concepts=%d provider=%s model=%s digest=%s\n", googleOKFReferenceAgentWorkflow, googleOKFReferenceAgentRevision, len(concepts), options.Provider, options.Model, bundle.Digest)
	return nil
}

type referenceAgentConcept struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Resource string `json:"resource"`
}

func referenceAgentConcepts(catalog readiness.SourceCatalog) []referenceAgentConcept {
	baseResource := catalog.Engine + "://" + catalog.Database + "/" + catalog.Schema
	concepts := []referenceAgentConcept{{ID: "datasets/" + catalog.Database, Path: "datasets/" + catalog.Database + ".md", Type: catalog.EngineTitle + " Dataset", Resource: baseResource}}
	for _, table := range catalog.Tables {
		concepts = append(concepts, referenceAgentConcept{ID: "tables/" + table.Name, Path: "tables/" + table.Name + ".md", Type: catalog.EngineTitle + " Table", Resource: baseResource + "/" + table.Name})
	}
	sort.Slice(concepts, func(i, j int) bool { return concepts[i].ID < concepts[j].ID })
	return concepts
}

func referenceAgentRawConcept(catalog readiness.SourceCatalog, concept referenceAgentConcept) (json.RawMessage, error) {
	if strings.HasPrefix(concept.ID, "datasets/") {
		return json.Marshal(struct {
			Database      string                   `json:"database"`
			Schema        string                   `json:"schema"`
			Engine        string                   `json:"engine"`
			Specification string                   `json:"specification,omitempty"`
			Generator     string                   `json:"generator"`
			Tables        []readiness.CatalogTable `json:"tables"`
		}{catalog.Database, catalog.Schema, catalog.Engine, catalog.Specification, catalog.Generator, catalog.Tables})
	}
	name := strings.TrimPrefix(concept.ID, "tables/")
	for _, table := range catalog.Tables {
		if table.Name == name {
			return json.Marshal(table)
		}
	}
	return nil, fmt.Errorf("source catalog lacks reference-agent concept %q", concept.ID)
}

func referenceAgentPrompt(catalog readiness.SourceCatalog, concept referenceAgentConcept, repair string) (string, error) {
	tableNames := make([]string, 0, len(catalog.Tables))
	for _, table := range catalog.Tables {
		tableNames = append(tableNames, table.Name)
	}
	sort.Strings(tableNames)
	resource := catalog.Engine + "://" + catalog.Database + "/" + catalog.Schema
	return fmt.Sprintf(`Generate agent-ready Open Knowledge Format (OKF v0.2) knowledge for my %s
source catalog %q (database %q, schema %q). This bundle covers one dataset and
these %d catalog tables: %s.

This isolated turn owns exactly %q (OKF type %q). Produce its one source-backed
concept document through the loaded Google reference-agent skill. The source
adapter is the authority for physical catalog facts; its existing document is
the catalog-derived starting point. The semantic adapter is the authority for
the generated Ossie model's descriptions, relationships, and metrics. Persist
the result with the skill's required write tool and do not return the document
as chat text. This task is one part of the complete catalog knowledge bundle,
so make links only to concepts the adapter reports.

Prior validator feedback (empty on first invocation): %s`, catalog.EngineTitle, resource, catalog.Database, catalog.Schema, len(tableNames), strings.Join(tableNames, ", "), concept.ID, concept.Type, repair), nil
}

type referenceAgentToolContext struct {
	Concept       referenceAgentConcept   `json:"concept"`
	Concepts      []referenceAgentConcept `json:"concepts"`
	Raw           json.RawMessage         `json:"raw"`
	Semantic      json.RawMessage         `json:"semantic"`
	KnowledgeRoot string                  `json:"knowledge_root"`
	WritesRoot    string                  `json:"writes_root"`
}

func writeReferenceAgentToolContext(root, knowledgeRoot string, concept referenceAgentConcept, concepts []referenceAgentConcept, raw, semantic json.RawMessage) (string, string, error) {
	context := referenceAgentToolContext{Concept: concept, Concepts: concepts, Raw: raw, Semantic: semantic, KnowledgeRoot: knowledgeRoot, WritesRoot: filepath.Join(root, "writes")}
	body, err := json.Marshal(context)
	if err != nil {
		return "", "", err
	}
	name := strings.ReplaceAll(concept.ID, "/", "-") + ".json"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", "", err
	}
	writePath := filepath.Join(context.WritesRoot, filepath.FromSlash(strings.TrimSuffix(concept.Path, ".md")+".json"))
	return path, writePath, nil
}

var (
	sqlCodeFence = regexp.MustCompile("(?is)```sql\\s*(.*?)```")
)

// validateReferenceAgentBody keeps generated knowledge free of query answers.
// Physical catalog facts and Ossie semantics are supplied through read tools;
// execution-time Agents, rather than this generation step, author SQL.
func validateReferenceAgentBody(body string) error {
	if sqlCodeFence.MatchString(body) {
		return fmt.Errorf("OKF knowledge must not include sample SQL; execution-time Agents generate SQL from the semantic material")
	}
	return nil
}

func runPiReferenceAgent(ctx context.Context, binary, provider, model, extension, contextPath, skill, prompt string, trace io.Writer) (string, error) {
	skillInstruction, err := os.ReadFile(filepath.Join(skill, "SKILL.md"))
	if err != nil {
		return "", fmt.Errorf("read Google OKF Agent Skill: %w", err)
	}
	command := exec.Command(binary, piReferenceAgentArgs(provider, model, extension, skill, string(skillInstruction), prompt)...)
	command.Env = append(os.Environ(), "S2SBENCH_OKFGEN_CONTEXT="+contextPath)
	if err := configureAgentProcess(command); err != nil {
		return "", err
	}
	var output bytes.Buffer
	outputWriter := io.Writer(&output)
	var debug *piDebugRenderer
	if candidate, ok := trace.(*piDebugRenderer); ok {
		debug = candidate
	}
	if trace != nil {
		outputWriter = io.MultiWriter(&output, trace)
	}
	command.Stdout = outputWriter
	command.Stderr = outputWriter
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start Pi: %w", err)
	}
	if debug != nil {
		defer debug.Flush()
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		_ = terminateAgentProcess(command.Process.Pid, false)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = terminateAgentProcess(command.Process.Pid, true)
			<-done
		}
		return "", fmt.Errorf("timed out: %w", ctx.Err())
	}
	if waitErr != nil {
		return "", fmt.Errorf("Pi failed: %w: %s", waitErr, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

func piReferenceAgentArgs(provider, model, extension, skill, skillInstruction, prompt string) []string {
	return []string{"--print", "--mode", "json", "--no-builtin-tools", "--extension", extension, "--skill", skill, "--append-system-prompt", skillInstruction, "--provider", provider, "--model", model, "--thinking", "low", "--approve", prompt}
}

// piDebugRenderer turns Pi's JSON event protocol into a compact, live terminal
// conversation. The raw protocol remains in the private captured output used
// for provenance; --debug deliberately renders only useful agent milestones.
type piDebugRenderer struct {
	output  io.Writer
	mu      sync.Mutex
	pending []byte
	segment string
}

func newPiDebugRenderer(output io.Writer) *piDebugRenderer {
	return &piDebugRenderer{output: output}
}

func (r *piDebugRenderer) Write(body []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, body...)
	for {
		index := bytes.IndexByte(r.pending, '\n')
		if index < 0 {
			break
		}
		r.renderLine(r.pending[:index])
		r.pending = r.pending[index+1:]
	}
	return len(body), nil
}

func (r *piDebugRenderer) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) > 0 {
		r.renderLine(r.pending)
		r.pending = nil
	}
	r.closeSegment()
}

func (r *piDebugRenderer) renderLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var event struct {
		Type                  string `json:"type"`
		AssistantMessageEvent struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			ToolCall struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"toolCall"`
		} `json:"assistantMessageEvent"`
		ToolName string          `json:"toolName"`
		Args     json.RawMessage `json:"args"`
		IsError  bool            `json:"isError"`
		Message  struct {
			StopReason string `json:"stopReason"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		r.closeSegment()
		fmt.Fprintf(r.output, "[pi] %s\n", line)
		return
	}
	switch event.Type {
	case "message_update":
		switch event.AssistantMessageEvent.Type {
		case "thinking_delta":
			r.writeDelta("thinking", "[pi thinking] ", event.AssistantMessageEvent.Delta)
		case "text_delta":
			r.writeDelta("text", "[pi] ", event.AssistantMessageEvent.Delta)
		case "thinking_end":
			r.closeSegment()
		case "text_end":
			r.closeSegment()
		case "toolcall_end":
			r.closeSegment()
			fmt.Fprintf(r.output, "[pi tool] → %s(%s)\n", event.AssistantMessageEvent.ToolCall.Name, debugToolArgs(event.AssistantMessageEvent.ToolCall.Name, event.AssistantMessageEvent.ToolCall.Arguments))
		}
	case "tool_execution_start":
		r.closeSegment()
		fmt.Fprintf(r.output, "[pi tool]   running %s(%s)\n", event.ToolName, debugToolArgs(event.ToolName, event.Args))
	case "tool_execution_end":
		r.closeSegment()
		status := "done"
		if event.IsError {
			status = "error"
		}
		fmt.Fprintf(r.output, "[pi tool] ← %s: %s\n", event.ToolName, status)
	case "message_end":
		r.closeSegment()
		if event.Message.StopReason != "" {
			fmt.Fprintf(r.output, "[pi] turn finished: %s\n", event.Message.StopReason)
		}
	}
}

func (r *piDebugRenderer) writeDelta(kind, prefix, delta string) {
	if delta == "" {
		return
	}
	if r.segment != kind {
		r.closeSegment()
		fmt.Fprint(r.output, prefix)
		r.segment = kind
	}
	fmt.Fprint(r.output, delta)
}

func (r *piDebugRenderer) closeSegment() {
	if r.segment != "" {
		fmt.Fprintln(r.output)
		r.segment = ""
	}
}

func compactJSON(value json.RawMessage) string {
	value = bytes.TrimSpace(value)
	if len(value) == 0 {
		return "{}"
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		return string(value)
	}
	return compact.String()
}

// debugToolArgs avoids flooding --debug output with an entire Markdown
// document while retaining enough information to follow the streamed turn.
func debugToolArgs(name string, value json.RawMessage) string {
	if name != "write_concept_doc" {
		return compactJSON(value)
	}
	var write struct {
		ConceptID string `json:"concept_id"`
		Body      string `json:"body"`
	}
	if err := json.Unmarshal(value, &write); err != nil {
		return compactJSON(value)
	}
	return fmt.Sprintf(`{"concept_id":%q,"body_bytes":%d}`, write.ConceptID, len(write.Body))
}

func loadReferenceAgentWrite(path string) (referenceAgentWrite, error) {
	file, err := os.Open(path)
	if err != nil {
		return referenceAgentWrite{}, fmt.Errorf("write_concept_doc was not called: %w", err)
	}
	defer file.Close()
	var write referenceAgentWrite
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&write); err != nil {
		return referenceAgentWrite{}, fmt.Errorf("decode write_concept_doc JSON: %w", err)
	}
	if strings.TrimSpace(write.ConceptID) == "" || len(write.Frontmatter) == 0 || strings.TrimSpace(write.Body) == "" {
		return referenceAgentWrite{}, fmt.Errorf("write_concept_doc arguments are incomplete")
	}
	return write, nil
}

func frozenKnowledgeBundle(root string, projection *readiness.Projection, catalogPath string, prompts, outputs []string, options okfgenOptions) (workload.KnowledgeBundle, error) {
	catalogDigest, err := workload.FileDigest(catalogPath)
	if err != nil {
		return workload.KnowledgeBundle{}, err
	}
	files := make([]workload.Asset, 0, len(projection.Files))
	for relative := range projection.Files {
		path := filepath.ToSlash(filepath.Join("knowledge", relative))
		digest, digestErr := workload.FileDigest(filepath.Join(root, filepath.FromSlash(path)))
		if digestErr != nil {
			return workload.KnowledgeBundle{}, digestErr
		}
		files = append(files, workload.Asset{Path: path, SHA256: digest, Format: "markdown"})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return workload.KnowledgeBundle{
		Format: "okf", Version: readiness.OKFVersion, Revision: readiness.OKFRevision, ProjectionVersion: readiness.CatalogProjectionVersion,
		Catalog: workload.Asset{Path: "catalog/catalog.json", SHA256: catalogDigest, Format: "json"}, Files: files,
		Enrichment: &workload.KnowledgeEnrichment{
			Workflow: googleOKFReferenceAgentWorkflow + "@" + googleOKFReferenceAgentRevision,
			Provider: strings.TrimSpace(options.Provider), Model: strings.TrimSpace(options.Model), Skills: append([]string(nil), googleOKFReferenceAgentSkills...),
			PromptSHA256: digestText(strings.Join(prompts, "\x00")), OutputSHA256: digestText(strings.Join(outputs, "\x00")),
		},
	}, nil
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeWorkloadBundle(root string, bundle workload.Bundle) error {
	temporary, err := os.CreateTemp(root, ".suite-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, filepath.Join(root, workload.ManifestFile))
}
