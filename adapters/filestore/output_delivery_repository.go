package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// OutputDeliveryRepository keeps the incident outputs' outbox beside the
// approval notifications': a pending delivery under
// <state>/deliveries/outputs/pending/<id>.json, one that is done under
// <state>/deliveries/outputs/done/.
type OutputDeliveryRepository struct {
	pending collection
	done    collection
}

// NewOutputDeliveryRepository stores deliveries under the state directory.
func NewOutputDeliveryRepository(stateDir string) OutputDeliveryRepository {
	dir := filepath.Join(stateDir, "deliveries", "outputs")
	return OutputDeliveryRepository{
		pending: collection{dir: filepath.Join(dir, "pending"), kind: "output delivery"},
		done:    collection{dir: filepath.Join(dir, "done"), kind: "output delivery"},
	}
}

// Save writes the delivery atomically where its status puts it, then
// removes it from the other place.
func (r OutputDeliveryRepository) Save(_ context.Context, delivery domain.OutputDelivery) error {
	to, from := r.pending, r.done
	if delivery.Status() != domain.DeliveryPending {
		to, from = r.done, r.pending
	}
	id := delivery.ID().String()
	if err := to.write(id, delivery.Snapshot()); err != nil {
		return err
	}
	stale, err := from.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(stale); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Get reads one delivery, pending or done.
func (r OutputDeliveryRepository) Get(_ context.Context, id domain.DeliveryID) (domain.OutputDelivery, error) {
	var snapshot domain.OutputDeliverySnapshot
	err := r.pending.read(id.String(), &snapshot)
	if domain.IsNotFound(err) {
		err = r.done.read(id.String(), &snapshot)
	}
	if err != nil {
		return domain.OutputDelivery{}, err
	}
	return domain.RestoreOutputDelivery(snapshot)
}

// ListPending reads every pending delivery, oldest first; a file that
// does not parse is set aside like any other record's.
func (r OutputDeliveryRepository) ListPending(_ context.Context) ([]domain.OutputDelivery, error) {
	ids, err := r.pending.ids()
	if err != nil {
		return nil, err
	}
	deliveries := make([]domain.OutputDelivery, 0, len(ids))
	for _, id := range ids {
		var snapshot domain.OutputDeliverySnapshot
		err := r.pending.read(id, &snapshot)
		if err == nil {
			var delivery domain.OutputDelivery
			if delivery, err = domain.RestoreOutputDelivery(snapshot); err == nil {
				deliveries = append(deliveries, delivery)
				continue
			}
		}
		if r.pending.skipListed(id, err) {
			continue
		}
		return nil, err
	}
	// Oldest first: the events of an incident in the order they happened.
	slices.SortStableFunc(deliveries, func(a, b domain.OutputDelivery) int {
		if c := a.CreatedAt().Compare(b.CreatedAt()); c != 0 {
			return c
		}
		return strings.Compare(a.ID().String(), b.ID().String())
	})
	return deliveries, nil
}
