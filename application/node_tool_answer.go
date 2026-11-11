package application

// NodeToolAnswer is what a tool the node serves in process answered a
// runtime without a model: its result as an object, or, when Refused is
// set, why it did not act (what a model would read as the tool's error).
type NodeToolAnswer struct {
	Object  map[string]any
	Refused string
}
