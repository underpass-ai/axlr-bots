package domain

// DeliveryStatus is where a notification delivery is: waiting for its
// next attempt, delivered, given up after its last attempt, or skipped
// because the wait it announces ended first.
type DeliveryStatus string

const (
	// DeliveryPending is a delivery with an attempt to come.
	DeliveryPending DeliveryStatus = "pending"
	// DeliveryDelivered is a delivery the target accepted.
	DeliveryDelivered DeliveryStatus = "delivered"
	// DeliveryFailed is a delivery whose every attempt failed.
	DeliveryFailed DeliveryStatus = "failed"
	// DeliverySkipped is a delivery no longer worth making.
	DeliverySkipped DeliveryStatus = "skipped"
)

// ParseDeliveryStatus validates a delivery status.
func ParseDeliveryStatus(raw string) (DeliveryStatus, error) {
	switch DeliveryStatus(raw) {
	case DeliveryPending, DeliveryDelivered, DeliveryFailed, DeliverySkipped:
		return DeliveryStatus(raw), nil
	}
	return "", invalid("delivery status", "must be pending, delivered, failed or skipped")
}

func (s DeliveryStatus) String() string { return string(s) }
