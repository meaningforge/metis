package observability

import (
	"context"
	"sync"
	"time"
)

type compilePipelineKey struct{}

// Pipeline projects compile lifecycle boundaries onto phase observations and
// sibling OpenTelemetry spans. It also satisfies planner.RuntimeObserver by
// method shape without making Planner depend on the application package.
type Pipeline struct {
	recorder *Recorder
	tracing  *Tracing
}

func NewPipeline(recorder *Recorder, tracing *Tracing) *Pipeline {
	return &Pipeline{recorder: recorder, tracing: tracing}
}

// Activate scopes phase instrumentation to a physical compile. Validate and
// Explain reuse planning code but are not compile operations and remain inert.
func (p *Pipeline) Activate(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, compilePipelineKey{}, true)
}

func (p *Pipeline) StartPlanning(ctx context.Context, dialect string) (context.Context, func(error)) {
	return p.StartPhase(ctx, PhasePlanning, NormalizeDialect(dialect))
}

func (p *Pipeline) StartOptimization(ctx context.Context, dialect string) (context.Context, func(bool, error)) {
	observed, finishPhase := p.StartPhase(ctx, PhaseOptimization, NormalizeDialect(dialect))
	var once sync.Once
	return observed, func(changed bool, err error) {
		once.Do(func() {
			finishPhase(err)
			if err != nil || p == nil || !pipelineActive(ctx) {
				return
			}
			outcome := OptimizerNoop
			if changed {
				outcome = OptimizerRewritten
			}
			p.recorder.Record(ctx, OptimizerObservation{Outcome: outcome})
		})
	}
}

func (p *Pipeline) StartPhase(ctx context.Context, phase Phase, dialect Dialect) (context.Context, func(error)) {
	if p == nil || !pipelineActive(ctx) || !validPhase(phase) || !validDialect(dialect) {
		return ctx, func(error) {}
	}
	started := time.Now()
	observed, span := p.tracing.Start(ctx, operationForPhase(phase))
	var once sync.Once
	return observed, func(err error) {
		once.Do(func() {
			if err != nil {
				p.tracing.RecordError(observed, err, ErrorCode(err))
			}
			p.recorder.Record(observed, PhaseObservation{
				Phase: phase, Dialect: dialect, Duration: time.Since(started),
			})
			span.End()
		})
	}
}

func pipelineActive(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(compilePipelineKey{}).(bool)
	return active
}

func operationForPhase(phase Phase) Operation {
	switch phase {
	case PhaseAnalysis:
		return OperationAnalysis
	case PhasePlanning:
		return OperationPlanning
	case PhaseOptimization:
		return OperationOptimization
	case PhaseRendering:
		return OperationRendering
	default:
		return ""
	}
}
