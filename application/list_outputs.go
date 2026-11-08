package application

import "context"

// ListOutputs answers the output targets without their secrets.
type ListOutputs struct {
	deps Dependencies
}

// NewListOutputs wires the use case.
func NewListOutputs(deps Dependencies) ListOutputs { return ListOutputs{deps: deps} }

// Execute lists the targets in name order.
func (uc ListOutputs) Execute(ctx context.Context) ([]OutputView, error) {
	targets, err := uc.deps.Outputs.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]OutputView, 0, len(targets))
	for _, target := range targets {
		views = append(views, NewOutputView(target))
	}
	return views, nil
}
