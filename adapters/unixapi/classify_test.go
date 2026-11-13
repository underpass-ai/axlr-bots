package unixapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestClassifyAnswersTheKindAndWhatGoesWithIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind string
	}{
		{"not found", domain.NotFoundError{Kind: "task", ID: "t-1"}, KindNotFound},
		{"validation", domain.ValidationError{}, KindValidation},
		{"state", domain.StateError{Subject: "x", Current: "a", Attempted: "b"}, KindState},
		{"conflict", domain.ConflictError{Kind: "bot", ID: "b"}, KindConflict},
		{"forbidden", domain.ForbiddenError{Reason: "no", Revoked: true}, KindForbidden},
		{"unauthorized", UnauthorizedError{}, KindUnauthorized},
		{"too large", &http.MaxBytesError{Limit: 1}, KindTooLarge},
		{"approval expired", domain.ApprovalExpiredError{TaskID: "t", CallID: "c", Deadline: time.Unix(1, 0)}, KindApprovalExpired},
		{"anything else", errors.New("boom"), KindInternal},
		{"wrapped", fmt.Errorf("while reading: %w", domain.NotFoundError{Kind: "task", ID: "t-1"}), KindNotFound},
	} {
		if got := Classify(tc.err); got.Kind != tc.kind || got.Message == "" {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
	if got := Classify(domain.NotFoundError{Kind: "task", ID: "t-1"}); got.Resource != "task" || got.ID != "t-1" {
		t.Errorf("not found carries what is missing: %+v", got)
	}
	if got := Classify(domain.ForbiddenError{Reason: "no", Revoked: true}); !got.Revoked {
		t.Errorf("forbidden carries revoked: %+v", got)
	}
}
