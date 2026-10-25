package unixapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RemoteError is an error the daemon reported, rebuilt on the client
// side: it keeps the kind so callers can tell a missing task from a
// refused state, and prints the daemon's message.
type RemoteError struct {
	Kind    string
	Message string
}

func (e RemoteError) Error() string { return e.Message }

// toWire classifies an error for the response.
func toWire(err error) (int, ErrorResponse) {
	var (
		notFound     domain.NotFoundError
		validation   domain.ValidationError
		state        domain.StateError
		conflict     domain.ConflictError
		forbidden    domain.ForbiddenError
		unauthorized UnauthorizedError
		tooLarge     *http.MaxBytesError
		expired      domain.ApprovalExpiredError
	)
	switch {
	case errors.As(err, &expired):
		return http.StatusConflict, ErrorResponse{Kind: KindApprovalExpired, Message: err.Error()}
	case errors.As(err, &notFound):
		return http.StatusNotFound, ErrorResponse{Kind: KindNotFound, Message: err.Error(), Resource: notFound.Kind, ID: notFound.ID}
	case errors.As(err, &forbidden):
		return http.StatusForbidden, ErrorResponse{Kind: KindForbidden, Message: err.Error()}
	case errors.As(err, &unauthorized):
		return http.StatusUnauthorized, ErrorResponse{Kind: KindUnauthorized, Message: err.Error()}
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, ErrorResponse{Kind: KindTooLarge, Message: "request body: " + err.Error()}
	case errors.As(err, &validation):
		return http.StatusBadRequest, ErrorResponse{Kind: KindValidation, Message: err.Error()}
	case errors.As(err, &state):
		return http.StatusConflict, ErrorResponse{Kind: KindState, Message: err.Error()}
	case errors.As(err, &conflict):
		return http.StatusConflict, ErrorResponse{Kind: KindConflict, Message: err.Error()}
	}
	return http.StatusInternalServerError, ErrorResponse{Kind: KindInternal, Message: err.Error()}
}

// fromWire rebuilds the error the daemon reported. A not-found error
// comes back as a domain.NotFoundError so IsNotFound keeps working, and
// a forbidden one as a domain.ForbiddenError so a node knows it was
// refused rather than unreachable.
func fromWire(status int, response ErrorResponse) error {
	if response.Message == "" {
		response.Message = fmt.Sprintf("the control plane answered HTTP %d", status)
	}
	if response.Kind == KindForbidden {
		return domain.ForbiddenError{Reason: strings.TrimPrefix(response.Message, "forbidden: ")}
	}
	if response.Kind == KindUnauthorized {
		return UnauthorizedError{}
	}
	if response.Kind == KindNotFound {
		if response.Resource == "" {
			response.Resource = "resource"
		}
		return domain.NotFoundError{Kind: response.Resource, ID: response.ID}
	}
	return RemoteError{Kind: response.Kind, Message: response.Message}
}
