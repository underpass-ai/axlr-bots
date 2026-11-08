package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RemoveOutput drops an output target. Its deliveries still pending are
// skipped at their next attempt: the target they go to is gone.
type RemoveOutput struct {
	deps Dependencies
	rec  recorder
}

// NewRemoveOutput wires the use case.
func NewRemoveOutput(deps Dependencies) RemoveOutput {
	return RemoveOutput{deps: deps, rec: recorder{deps: deps}}
}

// Execute removes the target and answers what it was.
func (uc RemoveOutput) Execute(ctx context.Context, raw string) (OutputView, error) {
	name, err := domain.NewOutputName(raw)
	if err != nil {
		return OutputView{}, err
	}
	target, err := uc.deps.Outputs.Get(ctx, name)
	if err != nil {
		return OutputView{}, err
	}
	if err := uc.deps.Outputs.Remove(ctx, name); err != nil {
		return OutputView{}, err
	}
	view := NewOutputView(target)
	details := map[string]string{"output": view.Name, "type": view.Type, "fingerprint": view.Fingerprint}
	if err := uc.rec.audit(ctx, "output.remove", view.Name, domain.AuditOK, details); err != nil {
		return OutputView{}, err
	}
	return view, nil
}
