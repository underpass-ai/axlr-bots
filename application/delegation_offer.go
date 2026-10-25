package application

// DelegateToolName is the tool the node serves to an execution whose bot
// may delegate; the control plane adds it to the execution's allowed
// tools, so the policy names it like any other.
const DelegateToolName = "axlr_bots_delegate"

// DelegationOffer tells the node that the execution's bot may delegate:
// the bots it may ask (what the tool's description lists), the task's
// depth and the chain's limit. The control plane checks every call
// again; the offer only describes the tool.
type DelegationOffer struct {
	Targets  []string
	Depth    int
	MaxDepth int
}
