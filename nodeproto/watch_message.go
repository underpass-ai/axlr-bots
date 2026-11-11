package nodeproto

// WatchMessage is what a node sends when a team's Jev watcher asks for
// the next question of its round: the cursor the last answer gave, empty
// to start. The watcher and its team are the execution's: the control
// plane reads them from its own records.
type WatchMessage struct {
	After string `json:"after,omitempty"`
}
