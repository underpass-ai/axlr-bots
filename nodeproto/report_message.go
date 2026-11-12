package nodeproto

import (
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ReportMessage is how an execution ended, as the node saw it: the
// contract's result, the usage, the policy it ran under, where the node
// kept the trace (a reference on the node, not a file the control plane
// can open) and the runtime's receipt when the build writes one. The
// artifacts the result lists were uploaded before the report
// (ArtifactHeaders); the control plane marks the ones it keeps.
type ReportMessage struct {
	Outcome        string                    `json:"outcome"`
	Result         domain.Result             `json:"result"`
	Usage          domain.Usage              `json:"usage"`
	FailureReason  string                    `json:"failureReason,omitempty"`
	TraceReference string                    `json:"traceReference,omitempty"`
	Policy         *domain.PolicyRecord      `json:"policy,omitempty"`
	Correlation    *domain.CorrelationRecord `json:"correlation,omitempty"`
	Receipt        json.RawMessage           `json:"receipt,omitempty"`
	// ContextRead is what the node read under the run's context keys, or
	// why it read nothing; sent only for a run that carried keys, so a
	// control plane that sends none is never handed the field.
	ContextRead []domain.ContextRead `json:"contextRead,omitempty"`
}
