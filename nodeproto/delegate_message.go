package nodeproto

// DelegateMessage is what a node sends when its execution asks another
// bot to do a task. The asking bot, its task and its depth are the
// execution's: the control plane reads them from its own records.
type DelegateMessage struct {
	Bot       string `json:"bot"`
	Objective string `json:"objective"`
	Wait      bool   `json:"wait,omitempty"`
}
