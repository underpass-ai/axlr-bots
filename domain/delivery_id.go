package domain

// DeliveryID identifies a notification delivery. It is derived from the
// wait and the target, so the receiver can drop a repeat.
type DeliveryID string

// NewDeliveryID validates a delivery identifier.
func NewDeliveryID(raw string) (DeliveryID, error) {
	value, err := validateIdentifier("delivery id", raw)
	if err != nil {
		return "", err
	}
	return DeliveryID(value), nil
}

func (id DeliveryID) String() string { return string(id) }
