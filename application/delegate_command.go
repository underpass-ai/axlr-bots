package application

// DelegateCommand is what a running execution asks through the node:
// that the bot do the objective. Wait is the node's business (it then
// polls the child); the control plane records it on the event.
type DelegateCommand struct {
	Bot       string
	Objective string
	Wait      bool
}
