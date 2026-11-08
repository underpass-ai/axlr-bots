package domain

import "slices"

const maxLadderNamespaces = 32

// LadderRunbook is one runbook of a ladder and its bounds: outside them
// the apply keeps its card whatever the runbook earned.
type LadderRunbook struct {
	name       RunbookName
	tool       ToolName
	namespaces []string
}

func newLadderRunbook(s LadderRunbookSnapshot) (LadderRunbook, error) {
	name, err := NewRunbookName(s.Name)
	if err != nil {
		return LadderRunbook{}, err
	}
	tool, err := NewToolName(s.Tool)
	if err != nil {
		return LadderRunbook{}, invalid("ladder runbook "+name.String(), err.Error())
	}
	if len(s.Namespaces) == 0 || len(s.Namespaces) > maxLadderNamespaces {
		return LadderRunbook{}, invalid("ladder runbook "+name.String(), "names 1 to 32 namespaces")
	}
	r := LadderRunbook{name: name, tool: tool}
	for _, raw := range s.Namespaces {
		namespace, err := validateIdentifier("ladder runbook namespace", raw)
		if err != nil {
			return LadderRunbook{}, err
		}
		if slices.Contains(r.namespaces, namespace) {
			return LadderRunbook{}, invalid("ladder runbook "+name.String(), "repeat the namespace "+namespace)
		}
		r.namespaces = append(r.namespaces, namespace)
	}
	return r, nil
}

// Name is the runbook's name.
func (r LadderRunbook) Name() RunbookName { return r.name }

// Tool is the one write tool the runbook may run alone.
func (r LadderRunbook) Tool() ToolName { return r.tool }

// Covers reports a namespace within the runbook's bounds.
func (r LadderRunbook) Covers(namespace string) bool { return slices.Contains(r.namespaces, namespace) }

// Titles reports the title is exactly "<runbook> in <namespace>": an
// alert name holding the separator names a runbook of its own, never the
// one before it.
func (r LadderRunbook) Titles(title, namespace string) bool {
	return title == r.name.String()+runbookTitleSeparator+namespace
}

func (r LadderRunbook) snapshot() LadderRunbookSnapshot {
	return LadderRunbookSnapshot{Name: r.name.String(), Tool: r.tool.String(), Namespaces: append([]string(nil), r.namespaces...)}
}
