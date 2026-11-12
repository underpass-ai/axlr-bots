package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// contextReadTimeout bounds one read of a team's store, the KMP server's
// start included.
const contextReadTimeout = 20 * time.Second

// readContexts reads, without a model, what the team's store on this node
// holds under each of the run's context keys
// (docs/architecture/team-subscriptions.md, "Done is a contract", E8d):
// only a key of the run's own memory team, in a store this node opens.
// Another team's key, a store the node does not open, a key the store
// does not have and a read that fails are each one disclosed line, never
// a failed run.
func (a Agent) readContexts(ctx context.Context, request application.ExecutionRequest) []domain.ContextRead {
	if len(request.Contexts) == 0 {
		return nil
	}
	reads := make([]domain.ContextRead, 0, len(request.Contexts))
	for _, ref := range request.Contexts {
		reads = append(reads, a.readContext(ctx, request, ref).Bounded())
	}
	return reads
}

func (a Agent) readContext(ctx context.Context, request application.ExecutionRequest, ref domain.ContextRef) domain.ContextRead {
	read := domain.ContextRead{Key: ref.Key, Team: ref.Team}
	team := domain.TeamName(ref.Team)
	switch {
	case team != request.MemoryTeam:
		read.Line = "held by team " + ref.Team + ", not yours: ask that team through the team channel, naming the key."
		return read
	case !(domain.NodeCapabilities{MemoryTeams: a.Options.MemoryTeams}).SharesMemoryOf(team):
		read.Line = "not read: this node does not open the memory of team " + ref.Team + "."
		return read
	case a.Context == nil:
		read.Line = "not read: this node reads no team memory (kmp-mcp not found)."
		return read
	}
	readCtx, cancel := context.WithTimeout(ctx, contextReadTimeout)
	defer cancel()
	lines, err := a.Context.ReadContext(readCtx, team, domain.ContextKey(ref.Key))
	switch {
	case errors.Is(err, application.ErrContextNotFound):
		read.Line = "the store of team " + ref.Team + " holds nothing under this key."
	case err != nil:
		read.Line = "could not be read: " + firstLine(err.Error())
	case len(lines) == 0:
		read.Line = "nothing recorded under this key yet."
	default:
		read.Read = lines
	}
	if err != nil {
		a.Options.log().Printf("node: execution %s: context %s of team %s: %v", request.ExecutionID, ref.Key, ref.Team, err)
	}
	return read
}

// withContexts gives the run what was read, in the one form its runtime
// takes: an LLM's prompt gets it under EarlierContextHeading, in what the
// prompt's limit leaves; a Jev program gets it as data.earlier in the
// task's input, the same fields, which its act receives when the program
// passes earlier on.
func withContexts(request application.ExecutionRequest, reads []domain.ContextRead) application.ExecutionRequest {
	if len(reads) == 0 {
		return request
	}
	if _, jev := request.Runtime.Jev(); jev {
		request.Input = inputWithEarlier(request.Input, reads)
		return request
	}
	text := request.PromptText()
	room := domain.MaxPromptLength - len(text) - len("\n\n")
	if block := domain.RenderContextReads(reads, room); block != "" {
		request.Prompt = domain.Prompt(text + "\n\n" + block)
	}
	return request
}

// inputWithEarlier puts the reads into the input's data as earlier,
// keeping every other field; an input that is no object is kept as it is.
func inputWithEarlier(raw json.RawMessage, reads []domain.ContextRead) json.RawMessage {
	input := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil || input == nil {
			return raw
		}
	}
	data, _ := input["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	data["earlier"] = reads
	input["data"] = data
	out, err := json.Marshal(input)
	if err != nil {
		return raw
	}
	return out
}

func firstLine(text string) string {
	text, _, _ = strings.Cut(strings.TrimSpace(text), "\n")
	return text
}
