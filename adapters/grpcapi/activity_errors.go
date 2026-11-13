package grpcapi

import (
	"context"
	"errors"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// activityError maps an error to a gRPC status with an ErrorDetail, as
// common.proto lists. It is private to the activity service: when the
// package's own error mapping lands, this one is replaced by it.
func activityError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
		return err
	}
	var (
		missing    domain.NotFoundError
		invalid    domain.ValidationError
		wrongState domain.StateError
	)
	code, detail := codes.Internal, &operatorv1.ErrorDetail{Kind: "internal", Message: err.Error()}
	switch {
	case errors.Is(err, context.Canceled):
		return status.FromContextError(err).Err()
	case errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.As(err, &missing):
		code, detail = codes.NotFound, &operatorv1.ErrorDetail{Kind: "not_found", Message: err.Error()}
	case errors.As(err, &invalid):
		code, detail = codes.InvalidArgument, &operatorv1.ErrorDetail{Kind: "validation", Message: err.Error()}
	case errors.As(err, &wrongState):
		code, detail = codes.FailedPrecondition, &operatorv1.ErrorDetail{Kind: "state", Message: err.Error()}
	}
	st := status.New(code, err.Error())
	if withDetail, derr := st.WithDetails(detail); derr == nil {
		st = withDetail
	}
	return st.Err()
}
