package nodeproto

// AskMessage is what a node sends when its execution asks another team a
// question. The asking bot and its task are the execution's: the control
// plane reads them from its own records.
type AskMessage struct {
	Team     string `json:"team"`
	Question string `json:"question"`
}
