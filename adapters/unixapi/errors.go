package unixapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// statusOfKind is the HTTP status an error kind is answered with.
var statusOfKind = map[string]int{
	application.KindApprovalExpired: http.StatusConflict,
	application.KindNotFound:        http.StatusNotFound,
	application.KindForbidden:       http.StatusForbidden,
	application.KindUnauthorized:    http.StatusUnauthorized,
	application.KindTooLarge:        http.StatusRequestEntityTooLarge,
	application.KindValidation:      http.StatusBadRequest,
	application.KindState:           http.StatusConflict,
	application.KindConflict:        http.StatusConflict,
}

// toWire classifies an error for the response.
func toWire(err error) (int, ErrorResponse) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge, ErrorResponse{Kind: application.KindTooLarge, Message: "request body: " + err.Error()}
	}
	c := application.ClassifyError(err)
	status, ok := statusOfKind[c.Kind]
	if !ok {
		status = http.StatusInternalServerError
	}
	return status, ErrorResponse{Kind: c.Kind, Message: c.Message, Resource: c.Resource, ID: c.ID, Revoked: c.Revoked}
}

// fromWire rebuilds the error the control plane reported. A not-found
// error comes back as a domain.NotFoundError so IsNotFound keeps working,
// and a forbidden one as a domain.ForbiddenError so a node knows it was
// refused rather than unreachable, and whether for good (revoked).
func fromWire(status int, response ErrorResponse) error {
	if response.Message == "" {
		response.Message = fmt.Sprintf("the control plane answered HTTP %d", status)
	}
	switch response.Kind {
	case application.KindForbidden:
		return domain.ForbiddenError{Reason: strings.TrimPrefix(response.Message, "forbidden: "), Revoked: response.Revoked}
	case application.KindUnauthorized:
		return application.UnauthorizedError{}
	case application.KindNotFound:
		if response.Resource == "" {
			response.Resource = "resource"
		}
		return domain.NotFoundError{Kind: response.Resource, ID: response.ID}
	}
	return application.RemoteError{Kind: response.Kind, Message: response.Message}
}
