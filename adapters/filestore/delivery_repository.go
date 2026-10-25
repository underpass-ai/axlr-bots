package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DeliveryRepository keeps the notification outbox: a pending delivery
// under <state>/deliveries/pending/<id>.json, one that is done
// (delivered, failed, skipped) under <state>/deliveries/done/, so the
// daemon's pass reads only what is still to be sent.
type DeliveryRepository struct {
	pending collection
	done    collection
}

// NewDeliveryRepository stores deliveries under the state directory.
func NewDeliveryRepository(stateDir string) DeliveryRepository {
	dir := filepath.Join(stateDir, "deliveries")
	return DeliveryRepository{
		pending: collection{dir: filepath.Join(dir, "pending"), kind: "delivery"},
		done:    collection{dir: filepath.Join(dir, "done"), kind: "delivery"},
	}
}

// Save writes the delivery atomically where its status puts it, then
// removes it from the other place.
func (r DeliveryRepository) Save(_ context.Context, delivery domain.Delivery) error {
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
func (r DeliveryRepository) Get(_ context.Context, id domain.DeliveryID) (domain.Delivery, error) {
	var snapshot domain.DeliverySnapshot
	err := r.pending.read(id.String(), &snapshot)
	if domain.IsNotFound(err) {
		err = r.done.read(id.String(), &snapshot)
	}
	if err != nil {
		return domain.Delivery{}, err
	}
	return domain.RestoreDelivery(snapshot)
}

// ListPending reads every pending delivery, in id order.
func (r DeliveryRepository) ListPending(_ context.Context) ([]domain.Delivery, error) {
	ids, err := r.pending.ids()
	if err != nil {
		return nil, err
	}
	deliveries := make([]domain.Delivery, 0, len(ids))
	for _, id := range ids {
		var snapshot domain.DeliverySnapshot
		if err := r.pending.read(id, &snapshot); err != nil {
			return nil, err
		}
		delivery, err := domain.RestoreDelivery(snapshot)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}
