package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// activityOptions maps the request's options; negative numbers are
// invalid.
func activityOptions(o *operatorv1.ActivityOptions) (application.ActivityOptions, error) {
	if o.GetMaxDepth() < 0 {
		return application.ActivityOptions{}, domain.ValidationError{Field: "options.max_depth", Reason: "must not be negative"}
	}
	if o.GetMaxCompletedItems() < 0 {
		return application.ActivityOptions{}, domain.ValidationError{Field: "options.max_completed_items", Reason: "must not be negative"}
	}
	return application.ActivityOptions{
		ExcludeChildren: o.GetExcludeChildren(), MaxDepth: int(o.GetMaxDepth()),
		SkipCompletedProgress: o.GetSkipCompletedProgress(), MaxCompletedItems: int(o.GetMaxCompletedItems()),
	}, nil
}
