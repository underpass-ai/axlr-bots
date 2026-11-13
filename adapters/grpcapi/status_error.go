package grpcapi

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// codes of the kinds the API names, as common.proto tabulates them.
var kindCodes = map[string]codes.Code{
	unixapi.KindNotFound:        codes.NotFound,
	unixapi.KindValidation:      codes.InvalidArgument,
	unixapi.KindState:           codes.FailedPrecondition,
	unixapi.KindApprovalExpired: codes.FailedPrecondition,
	unixapi.KindConflict:        codes.Aborted,
	unixapi.KindForbidden:       codes.PermissionDenied,
	unixapi.KindUnauthorized:    codes.Unauthenticated,
	unixapi.KindTooLarge:        codes.ResourceExhausted,
	unixapi.KindInternal:        codes.Internal,
}

// statusError is the gRPC status of an error: the code of its kind, its
// message and an ErrorDetail. A status already, and a context that ended,
// pass through as they are.
func statusError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, context.Canceled):
		return status.FromContextError(err).Err()
	case errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	}
	response := unixapi.Classify(err)
	code, ok := kindCodes[response.Kind]
	if !ok {
		code = codes.Internal
	}
	st := status.New(code, response.Message)
	detailed, detailErr := st.WithDetails(&operatorv1.ErrorDetail{
		Kind: response.Kind, Message: response.Message, Resource: response.Resource, Id: response.ID, Revoked: response.Revoked,
	})
	if detailErr != nil {
		return st.Err()
	}
	return detailed.Err()
}
