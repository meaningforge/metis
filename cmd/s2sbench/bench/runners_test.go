package s2sbench

import (
	"context"
	"fmt"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
)

type deterministicRawClient struct {
	generateCalls int
	repairCalls   int
}

func (c *deterministicRawClient) GenerateSQL(_ context.Context, task RawAssetsTask, _ TaskBudget) (RawAssetsResponse, error) {
	c.generateCalls++
	return c.answer(task)
}

func (c *deterministicRawClient) RepairSQL(_ context.Context, task RawAssetsTask, _ TaskBudget, failure string) (RawAssetsResponse, error) {
	c.repairCalls++
	if strings.TrimSpace(failure) == "" {
		return RawAssetsResponse{}, fmt.Errorf("repair did not receive the prior failure")
	}
	return c.answer(task)
}

func (c *deterministicRawClient) answer(task RawAssetsTask) (RawAssetsResponse, error) {
	if len(task.Models) < 2 || strings.TrimSpace(task.PhysicalSchema) == "" {
		return RawAssetsResponse{}, fmt.Errorf("raw path did not receive both semantic and physical assets")
	}
	name, ok := scenarioNameForQuestion(task.Question)
	if !ok {
		return RawAssetsResponse{}, fmt.Errorf("unknown frozen question %q", task.Question)
	}
	return RawAssetsResponse{
		SQL:   "scenario:" + name,
		Usage: ModelUsage{ToolCalls: 1, ContextTokens: 100, OutputTokens: 20},
	}, nil
}

type deterministicMetisClient struct {
	generateCalls int
	repairCalls   int
}

func (c *deterministicMetisClient) GenerateSemanticQuery(_ context.Context, task MetisTask, _ TaskBudget) (MetisResponse, error) {
	c.generateCalls++
	return c.answer(task)
}

func (c *deterministicMetisClient) RepairSemanticQuery(_ context.Context, task MetisTask, _ TaskBudget, failure string) (MetisResponse, error) {
	c.repairCalls++
	if strings.TrimSpace(failure) == "" {
		return MetisResponse{}, fmt.Errorf("repair did not receive the prior failure")
	}
	return c.answer(task)
}

func (c *deterministicMetisClient) answer(task MetisTask) (MetisResponse, error) {
	if strings.TrimSpace(task.Project) == "" {
		return MetisResponse{}, fmt.Errorf("Metis path did not receive its project namespace")
	}
	name, ok := scenarioNameForQuestion(task.Question)
	if !ok {
		return MetisResponse{}, fmt.Errorf("unknown frozen question %q", task.Question)
	}
	scenario, ok := scenarios.ByName(name)
	if !ok {
		return MetisResponse{}, fmt.Errorf("unknown scenario %q", name)
	}
	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		return MetisResponse{}, fmt.Errorf("unknown fixture %q", scenario.Fixture)
	}
	return MetisResponse{
		Query: query.SemanticQuery{Model: definition.Model, Metrics: []query.MetricRef{{Name: name}}},
		Usage: ModelUsage{ToolCalls: 1, ContextTokens: 80, OutputTokens: 15},
	}, nil
}

type deterministicSemanticCompiler struct {
	calls int
}

func (c *deterministicSemanticCompiler) Compile(_ context.Context, req service.CompileRequest) (*artifact.CompiledQuery, error) {
	c.calls++
	if strings.TrimSpace(req.Query.Project) == "" || strings.TrimSpace(req.Query.Model) == "" {
		return nil, fmt.Errorf("semantic query was not scoped to the canonical fixture")
	}
	if len(req.Query.Metrics) != 1 {
		return nil, fmt.Errorf("test compiler expected one marker metric, got %d", len(req.Query.Metrics))
	}
	return &artifact.CompiledQuery{
		SQLRenderResult: sql.SQLRenderResult{SQL: "scenario:" + req.Query.Metrics[0].Name},
	}, nil
}

func TestRunnerAdaptersExerciseTheSameExecutionAndOracle(t *testing.T) {
	budget := FrozenBudget
	budget.Repetitions = 1 // deterministic harness check, not a collectable v0 run

	rawClient := &deterministicRawClient{}
	rawRunner := &RawAssetsRunner{
		Client: rawClient,
		Assets: FixtureRawAssetsProvider{PhysicalSchema: func(_ context.Context, scenario scenarios.Scenario) (string, error) {
			return "CREATE TABLE fixture_" + string(scenario.Fixture) + " (...)", nil
		}},
	}
	metisClient := &deterministicMetisClient{}
	semanticCompiler := &deterministicSemanticCompiler{}
	metisRunner := &MetisRunner{Client: metisClient, Compiler: semanticCompiler}

	for _, runner := range []Runner{rawRunner, metisRunner} {
		t.Run(string(runner.Path()), func(t *testing.T) {
			execution := &deterministicExecution{}
			attempts, err := RunWithExecution(context.Background(), runner, execution, budget)
			if err != nil {
				t.Fatalf("RunWithExecution: %v", err)
			}
			if len(attempts) != 25 {
				t.Fatalf("attempt count = %d, want 25", len(attempts))
			}
			for _, attempt := range attempts {
				if attempt.Verdict != VerdictCorrect {
					t.Fatalf("%s verdict = %q: %s", attempt.Scenario, attempt.Verdict, attempt.Reason)
				}
				if runner.Path() == PathRawAssets && attempt.SemanticQuery != "" {
					t.Fatalf("raw path leaked semantic-query evidence on %s", attempt.Scenario)
				}
				if runner.Path() == PathMetis {
					if attempt.SemanticQuery == "" {
						t.Fatalf("Metis path lost semantic-query evidence on %s", attempt.Scenario)
					}
					if attempt.ToolCalls != 2 {
						t.Fatalf("Metis tool calls = %d, want model call count 1 + compile 1", attempt.ToolCalls)
					}
				}
			}
		})
	}

	if rawClient.generateCalls != 25 {
		t.Fatalf("raw generate calls = %d, want 25", rawClient.generateCalls)
	}
	if metisClient.generateCalls != 25 {
		t.Fatalf("Metis generate calls = %d, want 25", metisClient.generateCalls)
	}
	if semanticCompiler.calls != 25 {
		t.Fatalf("compile calls = %d, want 25", semanticCompiler.calls)
	}
}

func TestMetisRunnerReservesCompileFromTheFrozenToolBudget(t *testing.T) {
	client := &budgetProbeMetisClient{}
	runner := &MetisRunner{Client: client, Compiler: &deterministicSemanticCompiler{}}
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	question, err := QuestionFor(selected[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Answer(context.Background(), question, selected[0], FrozenBudget); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if client.seen.ToolCalls != FrozenBudget.ToolCalls-1 {
		t.Fatalf("client tool budget = %d, want %d after reserving compile", client.seen.ToolCalls, FrozenBudget.ToolCalls-1)
	}
}

type budgetProbeMetisClient struct {
	seen TaskBudget
}

func (c *budgetProbeMetisClient) GenerateSemanticQuery(_ context.Context, task MetisTask, budget TaskBudget) (MetisResponse, error) {
	c.seen = budget
	name, ok := scenarioNameForQuestion(task.Question)
	if !ok {
		return MetisResponse{}, fmt.Errorf("unknown question")
	}
	scenario, _ := scenarios.ByName(name)
	definition, _ := fixtures.Lookup(scenario.Fixture)
	return MetisResponse{Query: query.SemanticQuery{Model: definition.Model, Metrics: []query.MetricRef{{Name: name}}}}, nil
}

func (c *budgetProbeMetisClient) RepairSemanticQuery(context.Context, MetisTask, TaskBudget, string) (MetisResponse, error) {
	return MetisResponse{}, fmt.Errorf("unexpected repair")
}

func scenarioNameForQuestion(question string) (string, bool) {
	for name, frozen := range Questions {
		if frozen == question {
			return name, true
		}
	}
	return "", false
}
