package asyncapigen

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// node is a JSON value that keeps the order of an object's keys, so the
// generated fields follow the spec.
type node struct {
	keys   []string
	fields map[string]*node
	items  []*node
	scalar any
}

func parse(raw []byte) (*node, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	n, err := decode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("asyncapigen: trailing data after the spec")
	}
	return n, nil
}

func decode(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("asyncapigen: %w", err)
	}
	switch tok {
	case json.Delim('{'):
		n := &node{fields: map[string]*node{}}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("asyncapigen: %w", err)
			}
			child, err := decode(dec)
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, seen := n.fields[name]; !seen {
				n.keys = append(n.keys, name)
			}
			n.fields[name] = child
		}
		_, err = dec.Token()
		return n, err
	case json.Delim('['):
		n := &node{items: []*node{}}
		for dec.More() {
			child, err := decode(dec)
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, child)
		}
		_, err = dec.Token()
		return n, err
	}
	return &node{scalar: tok}, nil
}

// get walks keys from n; nil when a key is missing.
func (n *node) get(keys ...string) *node {
	for _, key := range keys {
		if n == nil || n.fields == nil {
			return nil
		}
		n = n.fields[key]
	}
	return n
}

// str is the node's string; empty when it is none.
func (n *node) str() string {
	if n == nil {
		return ""
	}
	s, _ := n.scalar.(string)
	return s
}
