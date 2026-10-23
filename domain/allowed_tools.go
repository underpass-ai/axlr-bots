package domain

import "sort"

// AllowedTools is the allow-list of tools a bot may call. An empty list
// allows nothing (fail closed). It is kept sorted and without duplicates.
type AllowedTools struct {
	names []ToolName
}

// NewAllowedTools validates every name and drops duplicates.
func NewAllowedTools(raw []string) (AllowedTools, error) {
	seen := make(map[ToolName]struct{}, len(raw))
	names := make([]ToolName, 0, len(raw))
	for _, r := range raw {
		name, err := NewToolName(r)
		if err != nil {
			return AllowedTools{}, err
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return AllowedTools{names: names}, nil
}

// Allows reports whether the list names the tool.
func (t AllowedTools) Allows(name ToolName) bool {
	for _, n := range t.names {
		if n == name {
			return true
		}
	}
	return false
}

// Names returns the list as plain strings, sorted.
func (t AllowedTools) Names() []string {
	out := make([]string, len(t.names))
	for i, n := range t.names {
		out[i] = string(n)
	}
	return out
}

// Len returns how many tools the list allows.
func (t AllowedTools) Len() int { return len(t.names) }
