package domain

import "sort"

// NodeLabels are the key=value pairs a placement selector matches:
// host=web-1, zone=eu, role=db.
type NodeLabels map[string]string

// NewNodeLabels validates labels: keys and values are identifiers.
func NewNodeLabels(raw map[string]string) (NodeLabels, error) {
	labels := make(NodeLabels, len(raw))
	for key, value := range raw {
		k, err := validateIdentifier("label key", key)
		if err != nil {
			return nil, err
		}
		v, err := validateIdentifier("label "+k, value)
		if err != nil {
			return nil, err
		}
		labels[k] = v
	}
	return labels, nil
}

// Keys lists the label keys, sorted.
func (l NodeLabels) Keys() []string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Copy returns the labels as a plain map.
func (l NodeLabels) Copy() map[string]string {
	if l == nil {
		return nil
	}
	out := make(map[string]string, len(l))
	for k, v := range l {
		out[k] = v
	}
	return out
}
