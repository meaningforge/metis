package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runOffline(args []string) int {
	if len(args) < 2 {
		offlineUsage()
		return 2
	}
	if args[1] == "help" || args[1] == "-h" || args[1] == "--help" {
		offlineUsage()
		return 0
	}
	switch args[0] {
	case "model":
		switch args[1] {
		case "validate":
			return validateModel(args[2:])
		case "inspect":
			return inspectModel(args[2:])
		case "format":
			return formatModel(args[2:])
		}
	case "project":
		switch args[1] {
		case "validate":
			return validateProject(args[2:])
		case "inspect":
			return inspectProject(args[2:])
		case "diff":
			return diffProject(args[2:])
		}
	case "query":
		if args[1] == "compile" {
			return genSQL(args[2:])
		}
	default:
		// The executable has no compatibility aliases for the retired CLI.
	}
	fmt.Fprintf(os.Stderr, "unknown command %q for metis %s\n", args[1], args[0])
	offlineUsage()
	return 2
}

func genSQL(args []string) int {
	fs := flag.NewFlagSet("metis query compile", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	modelFile := fs.String("model", "", "Apache Ossie YAML/JSON model file")
	semanticModel := fs.String("semantic-model", "", "semantic model name when the file contains multiple models")
	dialect := fs.String("dialect", "", "target SQL dialect")
	requestJSON := fs.String("request-json", "", "structured semantic query JSON file")
	output := fs.String("output", "", "write SQL and parameters as JSON to this file instead of stdout")
	timeRange := fs.String("time-range", "", "time range start,end; requires exactly one inferable time dimension")
	limit := fs.Int("limit", -1, "maximum number of rows")
	strict := fs.Bool("strict", false, "enable strict relationship and semantic validation")
	var metrics, dimensions, filters multiFlag
	fs.Var(&metrics, "metric", "metric name; may be specified multiple times")
	fs.Var(&dimensions, "dimension", "dimension name; may be specified multiple times")
	fs.Var(&filters, "filter", "filter expression such as \"order_month > '2026-01-01'\"; may be repeated")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *modelFile == "" || *dialect == "" {
		fmt.Fprintln(os.Stderr, "metis query compile: --model and --dialect are required")
		return 2
	}

	doc, err := LoadDocument(*modelFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: model parse failed: %v\n", err)
		return 1
	}

	var req QueryRequest
	if *requestJSON != "" {
		req, err = LoadRequestFile(*requestJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: request JSON failed: %v\n", err)
			return 1
		}
	}
	if *semanticModel != "" {
		req.Model = *semanticModel
	}
	req.Metrics = append(req.Metrics, metrics...)
	for _, dimension := range dimensions {
		req.Dimensions = append(req.Dimensions, NewDimension(dimension))
	}
	if *limit >= 0 {
		v := *limit
		req.Limit = &v
	}

	for _, raw := range filters {
		f, err := parseFilter(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return 2
		}
		req.Filters = append(req.Filters, f)
	}
	if *timeRange != "" {
		field, err := inferTimeField(doc, req.Model, req.Dimensions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: --time-range: %v\n", err)
			return 2
		}
		parts := strings.SplitN(*timeRange, ",", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			fmt.Fprintln(os.Stderr, "ERROR: --time-range must be start,end")
			return 2
		}
		req.Filters = append(req.Filters, Filter{Field: field, Op: "between", Value: []any{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}})
	}

	result, err := Compile(context.Background(), doc, strings.ToUpper(strings.TrimSpace(*dialect)), req, *strict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: semantic compilation failed: %v\n", err)
		return 2
	}

	text, err := renderOutput(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: encode query: %v\n", err)
		return 1
	}
	if *output != "" {
		if err := os.WriteFile(*output, []byte(text), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: write output: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Print(text)
	return 0
}

func validateModel(args []string) int {
	fs := flag.NewFlagSet("metis model validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	modelFile := fs.String("model", "", "Apache Ossie YAML/JSON model file")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *modelFile == "" {
		fmt.Fprintln(os.Stderr, "metis model validate: --model is required")
		return 2
	}
	doc, err := LoadDocument(*modelFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	fmt.Printf("valid: models=%d ontology_concepts=%d ontology_mappings=%d ossie_version=%s\n", len(doc.SemanticModel), len(doc.Ontology), len(doc.OntologyMappings), doc.Version)
	return 0
}

func inspectModel(args []string) int {
	fs := flag.NewFlagSet("metis model inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	modelFile := fs.String("model", "", "Apache Ossie YAML/JSON model file")
	semanticModel := fs.String("semantic-model", "", "semantic model name")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *modelFile == "" {
		fmt.Fprintln(os.Stderr, "metis model inspect: --model is required")
		return 2
	}
	doc, err := LoadDocument(*modelFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	name, model, err := selectModel(doc, *semanticModel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 2
	}
	out := struct {
		Model      string   `json:"model"`
		Metrics    []string `json:"metrics"`
		Dimensions []string `json:"dimensions"`
	}{Model: name}
	for _, metric := range model.Metrics {
		out.Metrics = append(out.Metrics, metric.Name)
	}
	for _, dataset := range model.Datasets {
		for _, field := range dataset.Fields {
			if field.Dimension != nil {
				out.Dimensions = append(out.Dimensions, field.Name)
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return 0
}

var filterRE = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(>=|<=|!=|<>|=|>|<)\s*(.*?)\s*$`)

func parseFilter(raw string) (Filter, error) {
	m := filterRE.FindStringSubmatch(raw)
	if len(m) != 4 {
		return Filter{}, fmt.Errorf("invalid --filter %q; expected: field OP value", raw)
	}
	value := strings.TrimSpace(m[3])
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		value = value[1 : len(value)-1]
		return Filter{Field: m[1], Op: m[2], Value: value}, nil
	}
	if b, err := strconv.ParseBool(value); err == nil {
		return Filter{Field: m[1], Op: m[2], Value: b}, nil
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return Filter{Field: m[1], Op: m[2], Value: n}, nil
	}
	return Filter{Field: m[1], Op: m[2], Value: value}, nil
}

func inferTimeField(doc *ossie.Document, requestedModel string, dimensions []Dimension) (string, error) {
	_, model, err := selectModel(doc, requestedModel)
	if err != nil {
		return "", err
	}
	requested := map[string]bool{}
	for _, d := range dimensions {
		requested[d.Name] = true
	}
	var candidates []string
	for _, dataset := range model.Datasets {
		for _, field := range dataset.Fields {
			if field.Dimension == nil {
				continue
			}
			isTime := field.Dimension.IsTime != nil && *field.Dimension.IsTime
			isTime = isTime || field.Datatype == ossie.DataTypeDate || field.Datatype == ossie.DataTypeTime || field.Datatype == ossie.DataTypeDateTime || field.Datatype == ossie.DataTypeDateTimeTz
			if isTime && (len(requested) == 0 || requested[field.Name]) {
				candidates = append(candidates, field.Name)
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no unique time dimension can be inferred")
	}
	return "", fmt.Errorf("multiple time dimensions match: %s", strings.Join(candidates, ", "))
}

func selectModel(doc *ossie.Document, requested string) (string, *ossie.SemanticModel, error) {
	if requested != "" {
		for i := range doc.SemanticModel {
			if doc.SemanticModel[i].Name == requested {
				return requested, &doc.SemanticModel[i], nil
			}
		}
		return "", nil, fmt.Errorf("semantic model %q not found", requested)
	}
	if len(doc.SemanticModel) == 1 {
		return doc.SemanticModel[0].Name, &doc.SemanticModel[0], nil
	}
	if len(doc.SemanticModel) == 0 {
		return "", nil, fmt.Errorf("no semantic models found")
	}
	return "", nil, fmt.Errorf("file contains %d semantic models; specify --semantic-model", len(doc.SemanticModel))
}

func renderOutput(result *Result) (string, error) {
	data, err := json.MarshalIndent(sql.SqlRenderResult{Dialect: sql.SQLDialect(result.Dialect), SQL: result.SQL, Parameters: result.Parameters}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func offlineUsage() {
	fmt.Fprintln(os.Stderr, "offline commands:")
	fmt.Fprintln(os.Stderr, "  metis query compile --model <ossie.yaml> --dialect <dialect> [query flags]")
	fmt.Fprintln(os.Stderr, "  metis model validate --model <ossie.yaml>")
	fmt.Fprintln(os.Stderr, "  metis model inspect --model <ossie.yaml>")
	fmt.Fprintln(os.Stderr, "  metis model format --model <ossie.yaml> --output <formatted.yaml>")
	fmt.Fprintln(os.Stderr, "  metis project validate --project <id> --config <project.yaml>")
	fmt.Fprintln(os.Stderr, "  metis project inspect --project <id> --config <project.yaml>")
	fmt.Fprintln(os.Stderr, "  metis project diff --project <id> --base-config <project.yaml> --candidate-config <project.yaml>")
}
