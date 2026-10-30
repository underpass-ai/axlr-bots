package domain

import (
	"regexp"
	"slices"
)

// DefaultJevModel is the Jev version a program that names none asks.
const DefaultJevModel = "jev-1.13.0"

// maxJevPass bounds the event fields a program passes on.
const maxJevPass = 8

var (
	jevArgument = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	jevServer   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	jevModel    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// JevProgram is a Jev bot's fixed steps. A task heard from a stop.*
// event calls the stop tool once. Any other task calls the start tool,
// then, while a result carries an ask {state, question, options}, puts
// it to Jev, calls the act tool with Jev's choice and calls the wait
// tool, which answers with the next ask; a result marked done ends the
// task. The world being acted on asks every question: the program only
// carries them to Jev and carries the answers back.
type JevProgram struct {
	server  string
	start   ToolName
	act     ToolName
	choice  string
	verdict string
	pass    []string
	wait    ToolName
	stop    ToolName
	tell    EventName
	tellOf  []string
	model   string
}

// jevTellFields are the decision's fields a tell may carry.
var jevTellFields = map[string]bool{"choice": true, "probability": true, "confidence": true}

// NewJevProgram validates a program.
func NewJevProgram(s JevProgramSnapshot) (JevProgram, error) {
	if !jevServer.MatchString(s.Server) {
		return JevProgram{}, invalid("jev server", "must name the node's MCP server that serves the tools (letters, digits, '.', '_', '-')")
	}
	var p JevProgram
	p.server = s.Server
	var err error
	if p.start, err = NewToolName(s.Start); err != nil {
		return JevProgram{}, invalid("jev start", "must name the tool that starts a task")
	}
	if p.act, err = NewToolName(s.Act.Tool); err != nil {
		return JevProgram{}, invalid("jev act tool", "must name the tool that acts on Jev's choice")
	}
	if p.wait, err = NewToolName(s.Wait); err != nil {
		return JevProgram{}, invalid("jev wait", "must name the tool that waits for the next question")
	}
	if s.Stop != "" {
		if p.stop, err = NewToolName(s.Stop); err != nil {
			return JevProgram{}, invalid("jev stop", "must name the tool a stop.* task calls")
		}
	}
	if !jevArgument.MatchString(s.Act.Choice) {
		return JevProgram{}, invalid("jev act choice", "must name the argument that takes Jev's choice")
	}
	if s.Act.Verdict != "" && (!jevArgument.MatchString(s.Act.Verdict) || s.Act.Verdict == s.Act.Choice) {
		return JevProgram{}, invalid("jev act verdict", "must name another argument, for Jev's verdict")
	}
	if len(s.Act.Pass) > maxJevPass {
		return JevProgram{}, invalid("jev act pass", "names at most 8 fields of the event's data")
	}
	for _, field := range s.Act.Pass {
		if !jevArgument.MatchString(field) || field == s.Act.Choice || field == s.Act.Verdict || slices.Contains(p.pass, field) {
			return JevProgram{}, invalid("jev act pass", "must name distinct fields of the event's data, apart from the choice and the verdict")
		}
		p.pass = append(p.pass, field)
	}
	p.choice, p.verdict = s.Act.Choice, s.Act.Verdict
	if s.Tell != nil {
		name, err := NewEventName(s.Tell.Name)
		if err != nil {
			return JevProgram{}, invalid("jev tell", "must name the event the bot tells after each act: "+err.Error())
		}
		if !KindOf(name).PublishedBy(PublisherBot) {
			return JevProgram{}, invalid("jev tell", KindOf(name).String()+".* is the control plane's own")
		}
		p.tell = name.Canonical()
		for _, field := range s.Tell.Fields {
			if !jevTellFields[field] && field != s.Act.Choice && !slices.Contains(p.pass, field) || slices.Contains(p.tellOf, field) {
				return JevProgram{}, invalid("jev tell", "fields are choice, probability, confidence, the act's choice argument or a field passed on, each once")
			}
			p.tellOf = append(p.tellOf, field)
		}
	}
	p.model = s.Model
	if p.model == "" {
		p.model = DefaultJevModel
	}
	if !jevModel.MatchString(p.model) || len(p.model) > 7 && p.model[len(p.model)-7:] == "-latest" {
		return JevProgram{}, invalid("jev model", "must be a pinned version such as jev-1.13.0")
	}
	return p, nil
}

// Server is the node's MCP server that serves the program's tools.
func (p JevProgram) Server() string { return p.server }

// Start is the tool a task begins with.
func (p JevProgram) Start() ToolName { return p.start }

// Act is the tool that acts on Jev's choice.
func (p JevProgram) Act() ToolName { return p.act }

// Choice is the act tool's argument that takes the choice.
func (p JevProgram) Choice() string { return p.choice }

// Verdict is the act tool's argument that takes Jev's verdict, or "".
func (p JevProgram) Verdict() string { return p.verdict }

// Pass is the event's data fields the act tool takes as they are.
func (p JevProgram) Pass() []string { return append([]string(nil), p.pass...) }

// Wait is the tool that waits for the next question.
func (p JevProgram) Wait() ToolName { return p.wait }

// Stop is the tool a stop.* task calls, or "" when the program has none.
func (p JevProgram) Stop() ToolName { return p.stop }

// Tell is the event the bot tells its teams after each act, with the
// fields of the decision it carries (all when none); false when the
// program tells nothing.
func (p JevProgram) Tell() (EventName, []string, bool) {
	return p.tell, append([]string(nil), p.tellOf...), p.tell != ""
}

// Model is the pinned Jev version the program asks.
func (p JevProgram) Model() string { return p.model }

// Tools are the tools the program calls.
func (p JevProgram) Tools() []ToolName {
	tools := []ToolName{p.start, p.act, p.wait}
	if p.stop != "" {
		tools = append(tools, p.stop)
	}
	return tools
}

// Snapshot is the program as it is stored.
func (p JevProgram) Snapshot() JevProgramSnapshot {
	snapshot := JevProgramSnapshot{
		Server: p.server, Start: p.start.String(), Wait: p.wait.String(), Stop: p.stop.String(), Model: p.model,
		Act: JevActSnapshot{Tool: p.act.String(), Choice: p.choice, Verdict: p.verdict, Pass: p.Pass()},
	}
	if p.tell != "" {
		snapshot.Tell = &JevTellSnapshot{Name: p.tell.String(), Fields: append([]string(nil), p.tellOf...)}
	}
	return snapshot
}
