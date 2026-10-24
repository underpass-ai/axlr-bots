package domain

// NodeID identifies a node: a host the node agent runs on.
type NodeID string

// NewNodeID validates a node identifier.
func NewNodeID(raw string) (NodeID, error) {
	value, err := validateIdentifier("node id", raw)
	if err != nil {
		return "", err
	}
	return NodeID(value), nil
}

func (id NodeID) String() string { return string(id) }
