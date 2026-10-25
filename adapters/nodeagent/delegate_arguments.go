package nodeagent

// delegateArguments are the arguments of axlr_bots_delegate; wait is
// true when absent.
type delegateArguments struct {
	Bot       string `json:"bot"`
	Objective string `json:"objective"`
	Wait      *bool  `json:"wait,omitempty"`
}

// waits reports whether the call waits for the child's answer.
func (a delegateArguments) waits() bool { return a.Wait == nil || *a.Wait }
