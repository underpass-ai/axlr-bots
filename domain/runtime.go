package domain

// Runtime is what runs a bot's tasks on its node: AXLR (the default) or
// a Jev program.
type Runtime struct {
	kind RuntimeKind
	jev  JevProgram
}

// NewRuntime validates a runtime; nil is AXLR. A jev runtime needs its
// program; an axlr one takes none.
func NewRuntime(s *RuntimeSnapshot) (Runtime, error) {
	if s == nil {
		return Runtime{kind: RuntimeAXLR}, nil
	}
	kind, err := ParseRuntimeKind(s.Kind)
	if err != nil {
		return Runtime{}, err
	}
	switch {
	case kind == RuntimeAXLR && s.Jev != nil:
		return Runtime{}, invalid("runtime", "an axlr runtime takes no jev program")
	case kind == RuntimeAXLR:
		return Runtime{kind: RuntimeAXLR}, nil
	case s.Jev == nil:
		return Runtime{}, invalid("runtime", "a jev runtime needs its program (runtime.jev)")
	}
	program, err := NewJevProgram(*s.Jev)
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{kind: RuntimeJev, jev: program}, nil
}

// Kind is the runtime's kind; the zero Runtime is AXLR.
func (r Runtime) Kind() RuntimeKind {
	if r.kind == "" {
		return RuntimeAXLR
	}
	return r.kind
}

// Jev is the program of a jev runtime.
func (r Runtime) Jev() (JevProgram, bool) { return r.jev, r.Kind() == RuntimeJev }

// Snapshot is the runtime as it is stored: nil for AXLR, so a bot that
// never named one reads as it always did.
func (r Runtime) Snapshot() *RuntimeSnapshot {
	if r.Kind() == RuntimeAXLR {
		return nil
	}
	program := r.jev.Snapshot()
	return &RuntimeSnapshot{Kind: RuntimeJev.String(), Jev: &program}
}

// fits refuses a runtime the bot's tools and level cannot carry: a Jev
// program calls only allowed tools, and calls them without asking a
// person, so its bot acts within the policy.
func (r Runtime) fits(tools AllowedTools, level AutonomyLevel) error {
	program, ok := r.Jev()
	if !ok {
		return nil
	}
	for _, tool := range program.Tools() {
		if !tools.Allows(tool) {
			return invalid("runtime", "the jev program calls "+tool.String()+", which is not an allowed tool")
		}
	}
	if level != AutonomyActWithinPolicy {
		return invalid("runtime", "a jev program calls its tools without asking a person: its bot's level is act-within-policy")
	}
	return nil
}
