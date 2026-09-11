package s2sbench

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
)

// ModelUsage is evidence reported by the model client for one attempt. ToolCalls
// counts only calls made by the client; the Metis adapter adds its mandatory
// compile call before returning the Attempt to the shared budget gate.
type ModelUsage struct {
	ToolCalls     int
	ContextTokens int
	OutputTokens  int
}

// RepairRunner is optional. RunWithExecution uses it when the first attempt
// fails to produce executable SQL or that SQL fails at the shared execution
// seam. The retry receives the error it caused and no oracle feedback.
type RepairRunner interface {
	Repair(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget, failure error) (Attempt, error)
}

type RawAssets struct {
	Project        string
	Models         []SemanticModelAsset
	PhysicalSchema string
}

// SemanticModelAsset is one canonical model in the complete raw semantic
// project exposed to path A. It intentionally contains no scenario identity.
type SemanticModelAsset struct {
	Model    string
	Document []byte
}

type RawAssetsProvider interface {
	Load(ctx context.Context, scenario scenarios.Scenario) (RawAssets, error)
}

// FixtureRawAssetsProvider binds path A to the exact embedded Ossie document
// used by conformance. PhysicalSchema is supplied by the execution backend so
// the model sees the schema of the engine it is actually writing SQL for.
type FixtureRawAssetsProvider struct {
	PhysicalSchema func(context.Context, scenarios.Scenario) (string, error)
}

func (p FixtureRawAssetsProvider) Load(ctx context.Context, scenario scenarios.Scenario) (RawAssets, error) {
	if _, ok := fixtures.Lookup(scenario.Fixture); !ok {
		return RawAssets{}, fmt.Errorf("unknown canonical fixture %q", scenario.Fixture)
	}
	if p.PhysicalSchema == nil {
		return RawAssets{}, fmt.Errorf("physical schema provider is required")
	}
	schema, err := p.PhysicalSchema(ctx, scenario)
	if err != nil {
		return RawAssets{}, err
	}
	if strings.TrimSpace(schema) == "" {
		return RawAssets{}, fmt.Errorf("physical schema is empty for fixture %q", scenario.Fixture)
	}
	definitions, err := fixtures.CanonicalSemanticModels()
	if err != nil {
		return RawAssets{}, err
	}
	models := make([]SemanticModelAsset, len(definitions))
	for i, definition := range definitions {
		models[i] = SemanticModelAsset{Model: definition.Model, Document: append([]byte(nil), definition.Document...)}
	}
	return RawAssets{Project: fixtures.ConformanceProject, Models: models, PhysicalSchema: schema}, nil
}

type RawAssetsTask struct {
	Question       string
	Project        string
	Models         []SemanticModelAsset
	PhysicalSchema string
}

type RawAssetsResponse struct {
	SQL   string
	Usage ModelUsage
}

// RawAssetsClient is the path-A model boundary. It receives only the frozen
// question plus raw semantic/physical assets; no Scenario or expected result is
// exposed across this boundary.
type RawAssetsClient interface {
	GenerateSQL(ctx context.Context, task RawAssetsTask, budget TaskBudget) (RawAssetsResponse, error)
	RepairSQL(ctx context.Context, task RawAssetsTask, budget TaskBudget, failure string) (RawAssetsResponse, error)
}

type RawAssetsRunner struct {
	Client RawAssetsClient
	Assets RawAssetsProvider
}

func (r *RawAssetsRunner) Path() Path { return PathRawAssets }

func (r *RawAssetsRunner) Answer(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget) (Attempt, error) {
	task, err := r.task(ctx, question, scenario)
	if err != nil {
		return Attempt{Index: 1}, err
	}
	if r.Client == nil {
		return Attempt{Index: 1}, fmt.Errorf("raw-assets model client is required")
	}
	response, err := r.Client.GenerateSQL(ctx, task, budget)
	return rawAttempt(1, response), err
}

func (r *RawAssetsRunner) Repair(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget, failure error) (Attempt, error) {
	task, err := r.task(ctx, question, scenario)
	if err != nil {
		return Attempt{Index: 2}, err
	}
	if r.Client == nil {
		return Attempt{Index: 2}, fmt.Errorf("raw-assets model client is required")
	}
	response, err := r.Client.RepairSQL(ctx, task, budget, failureText(failure))
	return rawAttempt(2, response), err
}

func (r *RawAssetsRunner) task(ctx context.Context, question string, scenario scenarios.Scenario) (RawAssetsTask, error) {
	if r == nil || r.Assets == nil {
		return RawAssetsTask{}, fmt.Errorf("raw-assets provider is required")
	}
	assets, err := r.Assets.Load(ctx, scenario)
	if err != nil {
		return RawAssetsTask{}, err
	}
	models := make([]SemanticModelAsset, len(assets.Models))
	for i, model := range assets.Models {
		models[i] = SemanticModelAsset{Model: model.Model, Document: append([]byte(nil), model.Document...)}
	}
	return RawAssetsTask{
		Question:       question,
		Project:        assets.Project,
		Models:         models,
		PhysicalSchema: assets.PhysicalSchema,
	}, nil
}

func rawAttempt(index int, response RawAssetsResponse) Attempt {
	return Attempt{
		Index:         index,
		SQL:           response.SQL,
		ToolCalls:     response.Usage.ToolCalls,
		ContextTokens: response.Usage.ContextTokens,
		OutputTokens:  response.Usage.OutputTokens,
	}
}

type MetisTask struct {
	Question string
	Project  string
}

type MetisResponse struct {
	Query query.SemanticQuery
	Usage ModelUsage
}

// MetisClient is the path-B model boundary. Its implementation may expose only
// Metis discovery/context tools to the model. It returns a semantic query, never
// SQL; the adapter below owns the compile call.
type MetisClient interface {
	GenerateSemanticQuery(ctx context.Context, task MetisTask, budget TaskBudget) (MetisResponse, error)
	RepairSemanticQuery(ctx context.Context, task MetisTask, budget TaskBudget, failure string) (MetisResponse, error)
}

type SemanticCompiler interface {
	Compile(ctx context.Context, req service.CompileRequest) (*artifact.CompiledQuery, error)
}

type MetisRunner struct {
	Client   MetisClient
	Compiler SemanticCompiler
	Dialect  sql.SQLDialect
}

func (r *MetisRunner) Path() Path { return PathMetis }

func (r *MetisRunner) Answer(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget) (Attempt, error) {
	return r.produce(ctx, question, scenario, budget, 1, nil)
}

func (r *MetisRunner) Repair(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget, failure error) (Attempt, error) {
	return r.produce(ctx, question, scenario, budget, 2, failure)
}

func (r *MetisRunner) produce(ctx context.Context, question string, scenario scenarios.Scenario, budget TaskBudget, index int, failure error) (Attempt, error) {
	if r == nil || r.Client == nil {
		return Attempt{Index: index}, fmt.Errorf("Metis model client is required")
	}
	if r.Compiler == nil {
		return Attempt{Index: index}, fmt.Errorf("Metis semantic compiler is required")
	}
	if _, ok := fixtures.Lookup(scenario.Fixture); !ok {
		return Attempt{Index: index}, fmt.Errorf("unknown canonical fixture %q", scenario.Fixture)
	}
	if budget.ToolCalls < 1 {
		return Attempt{Index: index}, fmt.Errorf("Metis path requires one tool call for compilation")
	}

	// Compilation itself is one of the frozen tool calls. Reserve it before
	// handing this turn's remaining shared budget to the model client.
	clientBudget := budget
	clientBudget.ToolCalls--
	task := MetisTask{Question: question, Project: fixtures.ConformanceProject}
	var response MetisResponse
	var err error
	if index == 1 {
		response, err = r.Client.GenerateSemanticQuery(ctx, task, clientBudget)
	} else {
		response, err = r.Client.RepairSemanticQuery(ctx, task, clientBudget, failureText(failure))
	}
	attempt := Attempt{
		Index:         index,
		ToolCalls:     response.Usage.ToolCalls,
		ContextTokens: response.Usage.ContextTokens,
		OutputTokens:  response.Usage.OutputTokens,
	}
	if err != nil {
		return attempt, err
	}
	if response.Usage.ToolCalls < 0 || response.Usage.ToolCalls > clientBudget.ToolCalls {
		return attempt, fmt.Errorf("Metis model used %d tool calls before compile, budget is 0..%d", response.Usage.ToolCalls, clientBudget.ToolCalls)
	}

	// Project is the harness-owned isolation namespace. Model selection belongs
	// to the Agent and is deliberately not inferred from the current scenario.
	response.Query.Project = task.Project
	if strings.TrimSpace(response.Query.Model) == "" {
		return attempt, fmt.Errorf("Metis model client did not select a semantic model")
	}
	encoded, err := json.Marshal(response.Query)
	if err != nil {
		return attempt, fmt.Errorf("encode semantic query evidence: %w", err)
	}
	attempt.SemanticQuery = string(encoded)
	attempt.ToolCalls++ // mandatory compile call, whether it succeeds or fails

	compiled, err := r.Compiler.Compile(ctx, service.CompileRequest{
		Query:   response.Query,
		Dialect: r.Dialect,
	})
	if err != nil {
		return attempt, err
	}
	if compiled == nil {
		return attempt, fmt.Errorf("Metis compile returned no physical query")
	}

	sqlQuery := compiled.PhysicalQuery
	if strings.TrimSpace(sqlQuery.SQL) == "" {
		return attempt, fmt.Errorf("Metis compile returned empty SQL")
	}
	attempt.SQL = sqlQuery.SQL
	attempt.Parameters = sqlQuery.Parameters
	return attempt, nil
}

func failureText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
