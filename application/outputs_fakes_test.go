package application

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// memoryOutputs is the fake OutputRepository.
type memoryOutputs struct {
	targets  []domain.OutputTarget
	listFail error
	addFail  error
}

func (m *memoryOutputs) List(context.Context) ([]domain.OutputTarget, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	out := slices.Clone(m.targets)
	slices.SortFunc(out, func(a, b domain.OutputTarget) int { return strings.Compare(a.Name().String(), b.Name().String()) })
	return out, nil
}

func (m *memoryOutputs) Get(ctx context.Context, name domain.OutputName) (domain.OutputTarget, error) {
	targets, err := m.List(ctx)
	if err != nil {
		return domain.OutputTarget{}, err
	}
	for _, target := range targets {
		if target.Name() == name {
			return target, nil
		}
	}
	return domain.OutputTarget{}, domain.NotFoundError{Kind: "output", ID: name.String()}
}

func (m *memoryOutputs) Add(_ context.Context, target domain.OutputTarget) error {
	if m.addFail != nil {
		return m.addFail
	}
	for _, existing := range m.targets {
		if existing.Name() == target.Name() {
			return domain.ConflictError{Kind: "output", ID: target.Name().String()}
		}
	}
	m.targets = append(m.targets, target)
	return nil
}

func (m *memoryOutputs) Remove(_ context.Context, name domain.OutputName) error {
	kept := slices.DeleteFunc(slices.Clone(m.targets), func(t domain.OutputTarget) bool { return t.Name() == name })
	if len(kept) == len(m.targets) {
		return domain.NotFoundError{Kind: "output", ID: name.String()}
	}
	m.targets = kept
	return nil
}

// memoryOutputDeliveries is the fake OutputDeliveryRepository.
type memoryOutputDeliveries struct {
	deliveries map[domain.DeliveryID]domain.OutputDelivery
	fail       error
	getFail    error
	listFail   error
}

func (m *memoryOutputDeliveries) Save(_ context.Context, d domain.OutputDelivery) error {
	if m.fail != nil {
		return m.fail
	}
	if m.deliveries == nil {
		m.deliveries = map[domain.DeliveryID]domain.OutputDelivery{}
	}
	m.deliveries[d.ID()] = d
	return nil
}

func (m *memoryOutputDeliveries) Get(_ context.Context, id domain.DeliveryID) (domain.OutputDelivery, error) {
	if m.getFail != nil {
		return domain.OutputDelivery{}, m.getFail
	}
	d, ok := m.deliveries[id]
	if !ok {
		return domain.OutputDelivery{}, domain.NotFoundError{Kind: "output delivery", ID: id.String()}
	}
	return d, nil
}

func (m *memoryOutputDeliveries) ListPending(context.Context) ([]domain.OutputDelivery, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	var out []domain.OutputDelivery
	for _, d := range m.deliveries {
		if d.Status() == domain.DeliveryPending {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b domain.OutputDelivery) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

// all lists every delivery, in creation order then id.
func (m *memoryOutputDeliveries) all() []domain.OutputDelivery {
	var out []domain.OutputDelivery
	for _, d := range m.deliveries {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b domain.OutputDelivery) int {
		if c := a.CreatedAt().Compare(b.CreatedAt()); c != 0 {
			return c
		}
		return strings.Compare(a.EventID().String(), b.EventID().String())
	})
	return out
}

// fakeOutputSender records each attempt and fails the first failures.
type fakeOutputSender struct {
	sent     []domain.OutputDelivery
	failures int
}

func (f *fakeOutputSender) Send(_ context.Context, _ domain.OutputTarget, d domain.OutputDelivery) error {
	f.sent = append(f.sent, d)
	if f.failures > 0 {
		f.failures--
		return errors.New("HTTP 503")
	}
	return nil
}
