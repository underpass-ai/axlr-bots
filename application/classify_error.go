package application

import (
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ClassifyError answers the kind of an error and what goes with it, as
// the operator APIs report it (the transport maps the kind to its own
// status).
func ClassifyError(err error) ErrorClassification {
	var (
		notFound     domain.NotFoundError
		validation   domain.ValidationError
		state        domain.StateError
		conflict     domain.ConflictError
		forbidden    domain.ForbiddenError
		unauthorized UnauthorizedError
		expired      domain.ApprovalExpiredError
	)
	switch {
	case errors.As(err, &expired):
		return ErrorClassification{Kind: KindApprovalExpired, Message: err.Error()}
	case errors.As(err, &notFound):
		return ErrorClassification{Kind: KindNotFound, Message: err.Error(), Resource: notFound.Kind, ID: notFound.ID}
	case errors.As(err, &forbidden):
		return ErrorClassification{Kind: KindForbidden, Message: err.Error(), Revoked: forbidden.Revoked}
	case errors.As(err, &unauthorized):
		return ErrorClassification{Kind: KindUnauthorized, Message: err.Error()}
	case errors.As(err, &validation):
		return ErrorClassification{Kind: KindValidation, Message: err.Error()}
	case errors.As(err, &state):
		return ErrorClassification{Kind: KindState, Message: err.Error()}
	case errors.As(err, &conflict):
		return ErrorClassification{Kind: KindConflict, Message: err.Error()}
	}
	return ErrorClassification{Kind: KindInternal, Message: err.Error()}
}
