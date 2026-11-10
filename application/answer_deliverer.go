package application

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// answerSummary is how much of an answer bot.question.answered carries.
const answerSummary = 280

// answerDeliverer brings a question's answer back to the bot that asked,
// when the question's task ends, whichever way it ends
// (docs/architecture/team-channel.md, "The answer comes back"): a message
// into the asking task when it still runs on a node, else a new task of
// the asking bot, trigger question_answer. A task that ended without an
// answer (failed, expired, cancelled) brings back why. It runs once per
// question: bot.question.answered on the question's task says it did.
type answerDeliverer struct {
	deps Dependencies
	rec  recorder
}

func newAnswerDeliverer(deps Dependencies) answerDeliverer {
	return answerDeliverer{deps: deps, rec: recorder{deps: deps}}
}

// taskEnded delivers the answer of a question's task that ended; any
// other task is none of its business. It never fails the end of the task
// for a reason of the asker's (gone, paused): it audits it and stops; only
// a store that fails is an error.
func (d answerDeliverer) taskEnded(ctx context.Context, responder domain.Bot, task domain.Task, execution domain.Execution) error {
	if task.Trigger().Type != domain.TriggerQuestion || !task.Status().IsTerminal() {
		return nil
	}
	if delivered, err := d.delivered(ctx, task.ID()); err != nil || delivered {
		return err
	}
	lineage, ok := task.Lineage()
	if !ok {
		return d.refuse(ctx, task, "the question has no asker")
	}
	asker, err := d.deps.Bots.Get(ctx, domain.BotID(lineage.ParentBotID))
	if domain.IsNotFound(err) {
		return d.refuse(ctx, task, "its asker "+lineage.ParentBotID+" is gone")
	}
	if err != nil {
		return err
	}
	parent, err := d.deps.Tasks.Get(ctx, domain.TaskID(lineage.ParentTaskID))
	if err != nil && !domain.IsNotFound(err) {
		return err
	}
	team := memoryTeamOf(responder)
	answered := task.Status() == domain.TaskCompleted
	text := task.FailureReason()
	if answered {
		text = execution.Result().Answer
	} else if text == "" {
		text = execution.FailureReason()
	}
	nonce, err := channelNonce()
	if err != nil {
		return err
	}
	framed, err := domain.AnswerFraming{Team: team, By: responder.ID(), Question: task.ID(), Answered: answered, Text: text, Nonce: nonce}.Render()
	if err != nil {
		return err
	}
	partial, err := d.partial(ctx, responder, domain.TeamName(task.Trigger().Name))
	if err != nil {
		return err
	}
	facts := map[string]any{
		"questionTaskId": task.ID().String(), "team": team.String(), "responder": responder.ID().String(), "status": task.Status().String(),
		"answered": answered, "store": "kmp/" + team.String(), "partial": partial,
	}
	if execution.NodeID() != "" {
		facts["node"] = execution.NodeID().String()
	}
	if !answered {
		facts["reason"] = text
	}
	delivery, delivered, err := d.toRunningAsker(ctx, asker, parent, framed, facts)
	if err != nil {
		return err
	}
	if !delivered {
		if !asker.AcceptsTasks() {
			return d.refuse(ctx, task, "its asker "+asker.ID().String()+" is "+asker.Status().String()+" and takes no task")
		}
		if delivery, err = d.toNewTask(ctx, asker, task, framed, facts); err != nil {
			return err
		}
	}
	return d.recordAnswered(ctx, asker, responder, parent, task, execution, text, facts, delivery)
}

// delivered reports whether the question's answer went back already.
func (d answerDeliverer) delivered(ctx context.Context, question domain.TaskID) (bool, error) {
	events, err := d.deps.EventReader.ListTaskEvents(ctx, question, MaxEventLimit)
	if err != nil {
		return false, err
	}
	for _, event := range events {
		if event.Type == domain.EventQuestionAnswered {
			return true, nil
		}
	}
	return false, nil
}

// toRunningAsker puts the answer into the asking task as a line of its
// chat, when that very task still runs on a node: never into another task
// of the asker.
func (d answerDeliverer) toRunningAsker(ctx context.Context, asker domain.Bot, parent domain.Task, framed string, facts map[string]any) (map[string]any, bool, error) {
	if parent.ID() == "" || parent.Status() != domain.TaskRunning || parent.CurrentExecutionID() == "" {
		return nil, false, nil
	}
	execution, err := d.deps.Executions.Get(ctx, parent.CurrentExecutionID())
	if err != nil {
		return nil, false, err
	}
	if execution.Outcome().IsFinal() || execution.NodeID() == "" {
		return nil, false, nil
	}
	payload := map[string]any{"text": framed, "from": "question:" + facts["questionTaskId"].(string), "data": facts}
	id, err := d.rec.executionEventID(ctx, domain.EventTaskMessage, asker, parent.ID(), execution.ID(), parent.CorrelationID(), payload)
	if err != nil {
		return nil, false, err
	}
	return map[string]any{"mode": "message", "taskId": parent.ID().String(), "messageId": id.String()}, true, nil
}

// toNewTask gives the asker a task of its own with the answer: the asking
// task has ended. It carries the question's trace and runs on the asker's
// own budgets.
func (d answerDeliverer) toNewTask(ctx context.Context, asker domain.Bot, question domain.Task, framed string, facts map[string]any) (map[string]any, error) {
	objective, err := domain.NewObjective(framed + "\nGo on with what your mission asks for this answer.")
	if err != nil {
		return nil, err
	}
	key, err := domain.NewIdempotencyKey("answer:" + question.ID().String())
	if err != nil {
		return nil, err
	}
	input, _ := json.Marshal(facts)
	created, err := submitTask(ctx, d.deps, submission{
		action: "task.question_answer", bot: asker, trigger: domain.TriggerRef{Type: domain.TriggerQuestionAnswer, Name: facts["team"].(string)},
		objective: objective, key: key, correlation: question.CorrelationID(), input: input,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"mode": "task", "taskId": created.Task.ID().String()}, nil
}

// partial reports whether the answering team's memory is split: more than
// one Online node could answer for it, so this answer read one store of
// several. Conservative: two nodes of one host that share a store count
// as two.
func (d answerDeliverer) partial(ctx context.Context, responder domain.Bot, asking domain.TeamName) (bool, error) {
	roster, err := readQuestionRoster(ctx, d.deps, responder.TenantID())
	if err != nil {
		return false, err
	}
	nodes := map[domain.NodeID]bool{}
	for _, bot := range roster.candidates(memoryTeamOf(responder), asking) {
		for _, node := range roster.nodes {
			if answersOn(bot, node) {
				nodes[node.ID()] = true
			}
		}
	}
	return len(nodes) > 1, nil
}

// recordAnswered publishes bot.question.answered on both sides, with the
// answer cut short, and audits the delivery.
func (d answerDeliverer) recordAnswered(ctx context.Context, asker, responder domain.Bot, parent, question domain.Task, execution domain.Execution, text string, facts, delivery map[string]any) error {
	summary := []rune(text)
	if len(summary) > answerSummary {
		summary = summary[:answerSummary]
	}
	payload := map[string]any{"delivery": delivery, "answer": string(summary)}
	for key, value := range facts {
		payload[key] = value
	}
	if err := d.rec.executionEvent(ctx, domain.EventQuestionAnswered, responder, question.ID(), execution.ID(), question.CorrelationID(), payload); err != nil {
		return err
	}
	if parent.ID() != "" {
		if err := d.rec.executionEvent(ctx, domain.EventQuestionAnswered, asker, parent.ID(), "", question.CorrelationID(), payload); err != nil {
			return err
		}
	}
	details := map[string]string{"question": question.ID().String(), "asker": asker.ID().String(), "mode": delivery["mode"].(string), "answered": strconv.FormatBool(facts["answered"].(bool))}
	return d.rec.audit(ctx, "question.answer", question.ID().String(), domain.AuditOK, details)
}

// refuse audits an answer that cannot go back and stops.
func (d answerDeliverer) refuse(ctx context.Context, question domain.Task, reason string) error {
	return d.rec.audit(ctx, "question.answer", question.ID().String(), domain.AuditDenied, map[string]string{"reason": reason})
}
