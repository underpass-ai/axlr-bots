package application

import (
	"encoding/json"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ApprovalNotificationSchema names the shape of an approval request
// notification; a receiver checks it before the fields.
const ApprovalNotificationSchema = "axlr-bots.approval-request/v1"

// ApprovalNotification is what a target receives when a task starts to
// wait for a person: the task, the bot, the pending call with its
// arguments, the deadline and what happens at it, the correlation ids,
// and the approve and deny actions on the operator API. DeliveryID is
// the same on every attempt, so a receiver can drop a repeat.
type ApprovalNotification struct {
	Schema        string                   `json:"schema"`
	DeliveryID    string                   `json:"deliveryId"`
	Event         string                   `json:"event"`
	Target        string                   `json:"target"`
	TenantID      string                   `json:"tenantId"`
	BotID         string                   `json:"botId"`
	BotName       string                   `json:"botName"`
	TaskID        string                   `json:"taskId"`
	ExecutionID   string                   `json:"executionId"`
	CorrelationID string                   `json:"correlationId"`
	NodeID        string                   `json:"nodeId,omitempty"`
	Objective     string                   `json:"objective"`
	Call          ApprovalNotificationCall `json:"call"`
	RequestedAt   time.Time                `json:"requestedAt"`
	Deadline      time.Time                `json:"deadline"`
	OnExpire      string                   `json:"onExpire"`
	Correlation   domain.CorrelationRecord `json:"correlation"`
	Actions       ApprovalActions          `json:"actions"`
}

// ApprovalNotificationCall is the pending call as the runtime reported
// it. Arguments larger than MaxNotifiedArguments are left out and
// ArgumentsOmitted says so; GET /v1/tasks/{id}/executions has them.
type ApprovalNotificationCall struct {
	Tool             string          `json:"tool"`
	CallID           string          `json:"callId"`
	SessionID        string          `json:"sessionId,omitempty"`
	Arguments        json.RawMessage `json:"arguments,omitempty"`
	ArgumentsOmitted bool            `json:"argumentsOmitted,omitempty"`
}

// MaxNotifiedArguments bounds the call's arguments a notification carries.
const MaxNotifiedArguments = 64 << 10

// ApprovalActions are how the person decides: a POST to each route of
// the operator API with its bearer token. Path is always there; URL when
// the control plane knows its operator address (serve --api-url).
type ApprovalActions struct {
	Approve ApprovalAction `json:"approve"`
	Deny    ApprovalAction `json:"deny"`
	Task    ApprovalAction `json:"task"`
}

// ApprovalAction is one route of the operator API.
type ApprovalAction struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	URL    string `json:"url,omitempty"`
}
