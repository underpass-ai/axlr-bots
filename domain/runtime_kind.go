package domain

// RuntimeKind is what runs a bot's tasks on its node.
type RuntimeKind string

const (
	// RuntimeAXLR runs a task as an AXLR session: a language model plans
	// the run and calls the tools. A bot that names no runtime has it.
	RuntimeAXLR RuntimeKind = "axlr"
	// RuntimeJev runs a task as a fixed program of tool calls on the
	// node, each decision one question to Jev: no language model.
	RuntimeJev RuntimeKind = "jev"
)

// ParseRuntimeKind validates a kind; the empty string is axlr.
func ParseRuntimeKind(raw string) (RuntimeKind, error) {
	switch RuntimeKind(raw) {
	case "", RuntimeAXLR:
		return RuntimeAXLR, nil
	case RuntimeJev:
		return RuntimeJev, nil
	}
	return "", invalid("runtime kind", "must be axlr or jev")
}

func (k RuntimeKind) String() string { return string(k) }
