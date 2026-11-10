package nodeagent

import (
	"context"
	"encoding/json"
	"log"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// asker carries out one execution's calls of axlr_bots_ask_team: it hands
// the question to the control plane over the node's channel and answers
// at once. It never waits for the answer, so the execution keeps its slot
// and goes on.
type asker struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	log       *log.Logger
}

// askedView is what the model reads about its question.
type askedView struct {
	Question string `json:"question"`
	Team     string `json:"team"`
	Bot      string `json:"answeredBy"`
	Status   string `json:"status"`
	Note     string `json:"note"`
}

// askArguments are the arguments of axlr_bots_ask_team.
type askArguments struct {
	Team     string `json:"team"`
	Question string `json:"question"`
}

func (a asker) call(ctx context.Context, raw json.RawMessage) mcpToolResult {
	var args askArguments
	if err := json.Unmarshal(raw, &args); err != nil || args.Team == "" || args.Question == "" {
		return textResult(`axlr_bots_ask_team takes {"team": "<team>", "question": "<the question, in full>"}`, true)
	}
	answer, err := a.transport.Ask(ctx, a.node, a.execution, application.AskTeamCommand{Team: args.Team, Question: args.Question})
	if err != nil {
		a.log.Printf("node: execution %s: question to %s failed: %v", a.execution, args.Team, err)
		return textResult("the control plane did not take the question: "+err.Error(), true)
	}
	if answer.Refused != nil {
		a.log.Printf("node: execution %s: question to %s refused (%s)", a.execution, args.Team, answer.Refused.Reason)
		return jsonResult(map[string]string{"refused": answer.Refused.Reason, "message": answer.Refused.Message}, true)
	}
	a.log.Printf("node: execution %s: asked %s, question %s for %s (created %v)", a.execution, args.Team, answer.TaskID, answer.BotID, answer.Created)
	note := "asked; the answer comes back later, as a message while you still run or as a new task once you have ended. Go on with your work."
	if !answer.Created {
		note = "you asked this already; it is the same question. The answer comes back later."
	}
	return jsonResult(askedView{Question: answer.TaskID.String(), Team: args.Team, Bot: answer.BotID.String(), Status: answer.Status.String(), Note: note}, false)
}
