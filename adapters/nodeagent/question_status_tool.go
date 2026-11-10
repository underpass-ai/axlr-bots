package nodeagent

import (
	"context"
	"encoding/json"
	"log"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// questionStatusTool describes axlr_bots_question_status: where a question
// the bot asked stands. It only reads; it never interrupts the bot that
// answers, and giving a question up is not the asker's to do.
func questionStatusTool() mcpTool {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{"type": "string", "description": "the question's id, as axlr_bots_ask_team answered it"},
		},
		"required":             []string{"question"},
		"additionalProperties": false,
	})
	description := "Read where a question you asked another team stands: queued and why, running since when and its last step, waiting for a person, how long is left. It only reads: it never interrupts the bot that answers and never gives the question up. The answer itself comes back to you on its own, as a message or a new task."
	return mcpTool{Name: application.QuestionStatusToolName, Description: description, InputSchema: schema}
}

// questionReader carries out one execution's calls of
// axlr_bots_question_status.
type questionReader struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	log       *log.Logger
}

// questionStatusView is what the model reads about its question.
type questionStatusView struct {
	Question string `json:"question"`
	Status   string `json:"status"`
	State    string `json:"state"`
}

func (q questionReader) call(ctx context.Context, raw json.RawMessage) mcpToolResult {
	var args struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.Question == "" {
		return textResult(`axlr_bots_question_status takes {"question": "<the question's id>"}`, true)
	}
	question, err := domain.NewTaskID(args.Question)
	if err != nil {
		return textResult("not a question id: "+err.Error(), true)
	}
	view, err := q.transport.QuestionState(ctx, q.node, q.execution, question)
	if err != nil {
		q.log.Printf("node: execution %s: reading question %s: %v", q.execution, question, err)
		return textResult("the control plane did not answer for that question: "+err.Error(), true)
	}
	return jsonResult(questionStatusView{Question: view.Question.String(), Status: view.Status.String(), State: view.Text()}, false)
}
