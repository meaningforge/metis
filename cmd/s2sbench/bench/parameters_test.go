package s2sbench

import (
	"context"
	"encoding/json"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/renderer/sql"
)

type parameterizedSemanticCompiler struct{}

func (parameterizedSemanticCompiler) Compile(_ context.Context, req service.CompileRequest) (*artifact.CompiledQuery, error) {
	return &artifact.CompiledQuery{
		SqlStatement: sql.SqlStatement{
			SQL: "SELECT ? AS scenario_name",
			Parameters: []sql.QueryParameter{
				{Value: req.Query.Metrics[0].Name},
			},
		},
	}, nil
}

func TestMetisRunnerPreservesCompiledSQLParameters(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	scenario := selected[0]
	question, err := QuestionFor(scenario.Name)
	if err != nil {
		t.Fatal(err)
	}

	runner := &MetisRunner{
		Client:   &deterministicMetisClient{},
		Compiler: parameterizedSemanticCompiler{},
	}
	attempt, err := runner.Answer(context.Background(), question, scenario, FrozenBudget)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	want := "SELECT ? AS scenario_name"
	if attempt.SQL != want || len(attempt.Parameters) != 1 || attempt.Parameters[0].Value != scenario.Name {
		t.Fatalf("SQL = %q, want parameterized %q", attempt.SQL, want)
	}
}

type bindingExecution struct {
	deterministicExecution
	sql        string
	parameters []sql.QueryParameter
}

func (e *bindingExecution) RunSQL(_ context.Context, statement string, parameters ...sql.QueryParameter) (scenarios.ResultSet, error) {
	e.sql, e.parameters = statement, parameters
	return scenarios.ResultSet{}, nil
}

func TestExecutionAndEvidencePreserveBindings(t *testing.T) {
	value := "O'Reilly\\path?\n"
	attempt := Attempt{SQL: "SELECT ?", Parameters: []sql.QueryParameter{{Value: value}}}
	execution := &bindingExecution{}
	runner := &executingRunner{runner: &MetisRunner{}, execution: execution}
	if err := runner.execute(context.Background(), scenarios.Scenario{Name: "bindings"}, &attempt, nil); err != nil {
		t.Fatal(err)
	}
	if execution.sql != attempt.SQL || len(execution.parameters) != 1 || execution.parameters[0].Value != value {
		t.Fatalf("lost binding: %#v", execution)
	}
	evidence := evidenceFor(attempt, 1, nil)
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AttemptEvidence
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SQL != attempt.SQL || len(decoded.Parameters) != 1 || decoded.Parameters[0].Value != value {
		t.Fatalf("lost evidence binding: %s", encoded)
	}
}
