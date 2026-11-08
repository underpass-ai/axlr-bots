package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AddOutput keeps a new output target. The audit entry names it, its
// type and its fingerprint, never its URL or secret.
type AddOutput struct {
	deps Dependencies
	rec  recorder
}

// NewAddOutput wires the use case.
func NewAddOutput(deps Dependencies) AddOutput {
	return AddOutput{deps: deps, rec: recorder{deps: deps}}
}

// Execute validates and keeps the target; a name already taken is a
// domain.ConflictError: remove it first to change it.
func (uc AddOutput) Execute(ctx context.Context, definition OutputDefinition) (OutputView, error) {
	target, err := definition.target()
	if err != nil {
		return OutputView{}, err
	}
	if err := uc.deps.Outputs.Add(ctx, target); err != nil {
		return OutputView{}, err
	}
	view := NewOutputView(target)
	details := map[string]string{"output": view.Name, "type": view.Type, "fingerprint": view.Fingerprint}
	if err := uc.rec.audit(ctx, "output.add", view.Name, domain.AuditOK, details); err != nil {
		return OutputView{}, err
	}
	return view, nil
}
