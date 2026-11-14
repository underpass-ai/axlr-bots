package grpcclient

import (
	"errors"
	"fmt"
	"github.com/underpass-ai/axlr-bots/application"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// fromStatus rebuilds the error the control plane reported, as the REST
// client did: a not-found as domain.NotFoundError, a forbidden as
// domain.ForbiddenError, an unauthorized as application.UnauthorizedError,
// any other kind as application.RemoteError with the control plane's own
// message. A failure with no ErrorDetail (the control plane did not
// answer: Unavailable, a deadline) says which address it was.
func fromStatus(address string, err error) error {
	if err == nil {
		return nil
	}
	var remote application.RemoteError
	if errors.As(err, &remote) || domain.IsNotFound(err) || domain.IsForbidden(err) {
		return err
	}
	failure, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("control plane at %s: %w", address, err)
	}
	for _, detail := range failure.Details() {
		if info, isDetail := detail.(*operatorv1.ErrorDetail); isDetail {
			return fromDetail(info)
		}
	}
	if kind := kindOf(failure.Code()); kind != "" {
		return fromDetail(&operatorv1.ErrorDetail{Kind: kind, Message: failure.Message()})
	}
	return fmt.Errorf("control plane at %s: %s", address, failure.Message())
}

// fromDetail is fromWire of the REST client over the status detail.
func fromDetail(detail *operatorv1.ErrorDetail) error {
	message := detail.GetMessage()
	if message == "" {
		message = "the control plane refused the call"
	}
	switch detail.GetKind() {
	case application.KindForbidden:
		return domain.ForbiddenError{Reason: strings.TrimPrefix(message, "forbidden: "), Revoked: detail.GetRevoked()}
	case application.KindUnauthorized:
		return application.UnauthorizedError{}
	case application.KindNotFound:
		resource := detail.GetResource()
		if resource == "" {
			resource = "resource"
		}
		return domain.NotFoundError{Kind: resource, ID: detail.GetId()}
	}
	return application.RemoteError{Kind: detail.GetKind(), Message: message}
}

// kindOf is the kind a gRPC code stands for when the status carries no
// detail: the table of common.proto read backwards. Codes of the
// transport itself have none.
func kindOf(code codes.Code) string {
	switch code {
	case codes.NotFound:
		return application.KindNotFound
	case codes.InvalidArgument:
		return application.KindValidation
	case codes.FailedPrecondition:
		return application.KindState
	case codes.Aborted:
		return application.KindConflict
	case codes.PermissionDenied:
		return application.KindForbidden
	case codes.Unauthenticated:
		return application.KindUnauthorized
	case codes.ResourceExhausted:
		return application.KindTooLarge
	case codes.Internal:
		return application.KindInternal
	}
	return ""
}
