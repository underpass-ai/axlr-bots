package domain

// Placement says which nodes may run a bot's tasks: named nodes, nodes
// whose labels match a selector, or any node when both are empty.
type Placement struct {
	nodes    []NodeID
	selector NodeLabels
}

// NewPlacement validates the node ids and the selector.
func NewPlacement(nodes []string, selector map[string]string) (Placement, error) {
	p := Placement{}
	for _, raw := range nodes {
		id, err := NewNodeID(raw)
		if err != nil {
			return Placement{}, err
		}
		p.nodes = append(p.nodes, id)
	}
	labels, err := NewNodeLabels(selector)
	if err != nil {
		return Placement{}, err
	}
	if len(labels) > 0 {
		p.selector = labels
	}
	return p, nil
}

// Nodes lists the named nodes, copied.
func (p Placement) Nodes() []NodeID { return append([]NodeID(nil), p.nodes...) }

// Selector is the label selector, copied.
func (p Placement) Selector() map[string]string { return p.selector.Copy() }

// IsEmpty reports a placement on any node.
func (p Placement) IsEmpty() bool { return len(p.nodes) == 0 && len(p.selector) == 0 }

// Admits reports whether the node may run the bot: named, or matching
// every selector label, or any node when the placement is empty.
func (p Placement) Admits(node Node) bool {
	if p.IsEmpty() {
		return true
	}
	for _, id := range p.nodes {
		if id == node.ID() {
			return true
		}
	}
	if len(p.selector) == 0 {
		return false
	}
	for key, value := range p.selector {
		if node.Labels()[key] != value {
			return false
		}
	}
	return true
}

// Snapshot returns the placement as plain data.
func (p Placement) Snapshot() PlacementSnapshot {
	s := PlacementSnapshot{Selector: p.selector.Copy()}
	for _, id := range p.nodes {
		s.Nodes = append(s.Nodes, id.String())
	}
	return s
}
