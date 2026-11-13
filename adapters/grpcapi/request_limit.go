package grpcapi

import "github.com/underpass-ai/axlr-bots/domain"

// limitOf is a limit field as the use cases take it: zero is the default,
// the use case bounds it above; a negative one is not a number of events.
func limitOf(field string, n int32) (int, error) {
	if n < 0 {
		return 0, domain.ValidationError{Field: field, Reason: "must not be negative"}
	}
	return int(n), nil
}
