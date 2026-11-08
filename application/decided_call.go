package application

// decidedCall is a decision of an apply task's call as the incident
// reads it: the outcome, the call, the tool as the runtime named it and
// the one it reaches, who decided and whether a named person did.
type decidedCall struct {
	outcome       string
	callID        string
	tool          string
	effectiveTool string
	by            string
	person        bool
}
