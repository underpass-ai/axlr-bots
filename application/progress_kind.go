package application

// ProgressKind is what one progress item tells about a running
// execution.
type ProgressKind string

// The kinds of progress item a node sends.
const (
	// ProgressToolStarted: the run asked for a tool call (its tool, the
	// MCP tool axlr_call_tool names, a short summary of its arguments when
	// the line carries them).
	ProgressToolStarted ProgressKind = "tool.started"
	// ProgressToolEnded: the call ended: ok, denied or failed, and how
	// long it took on the node's clock.
	ProgressToolEnded ProgressKind = "tool.ended"
	// ProgressDecisionRequired: the run stopped on a call that waits for
	// a person.
	ProgressDecisionRequired ProgressKind = "decision.required"
	// ProgressDelegation: the run asked another bot; the child task when
	// the control plane created one, the reason when it refused.
	ProgressDelegation ProgressKind = "delegation"
	// ProgressText: the model's words since the last item, coalesced.
	ProgressText ProgressKind = "text"
	// ProgressTextTruncated: the execution's text reached its cap; no
	// more text item follows.
	ProgressTextTruncated ProgressKind = "text.truncated"
)

// known reports a kind the control plane records.
func (k ProgressKind) known() bool {
	switch k {
	case ProgressToolStarted, ProgressToolEnded, ProgressDecisionRequired, ProgressDelegation, ProgressText, ProgressTextTruncated:
		return true
	}
	return false
}
