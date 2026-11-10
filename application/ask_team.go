package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AskTeam is a running execution asking, through its node, another team
// for context (docs/architecture/team-channel.md). The control plane
// derives the asker from the execution the node holds, never from the
// request. It checks, in the ADR's order, that the bot has a memory team,
// that the team asked is not its own, that its ask names that team, that
// the task is not itself a question, the rates and the team's queue, and
// that some member may answer; the first that fails is the refusal.
// Otherwise it picks one member and creates one task for it, trigger
// question, under the asker's trace id. Two questions never land on one
// task, since the pick runs under the runner's lock.
type AskTeam struct {
	deps Dependencies
	rec  recorder
}

// NewAskTeam wires the use case.
func NewAskTeam(deps Dependencies) AskTeam {
	return AskTeam{deps: deps, rec: recorder{deps: deps}}
}

// asking is one question being checked: the asker's side.
type asking struct {
	node      domain.NodeID
	execution domain.Execution
	task      domain.Task
	bot       domain.Bot
	from      domain.TeamName
	team      domain.TeamName
	rawTeam   string
	question  domain.Question
	wait      bool
}

// Execute creates the question's task, finds the one the same ask
// created, or answers why not.
func (uc AskTeam) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command AskTeamCommand) (QuestionAnswer, error) {
	a, err := uc.load(ctx, node, executionID, command)
	if err != nil {
		return QuestionAnswer{}, err
	}
	if a.from == "" {
		return uc.refuse(ctx, a, domain.RefusedNoTeam, "bot "+a.bot.ID().String()+" is in no team: it has no team to ask for")
	}
	if a.team != "" && a.bot.Teams().Has(a.team) {
		return uc.refuse(ctx, a, domain.RefusedOwnTeam, fmt.Sprintf("%s is a team of bot %s: inside a team, delegate to a teammate", a.team, a.bot.ID()))
	}
	if a.team == "" || !a.bot.Ask().Allows(a.team) {
		return uc.refuse(ctx, a, domain.RefusedNotAllowed, fmt.Sprintf("bot %s may not ask team %s; it may ask: %s", a.bot.ID(), a.rawTeam, joinNames(teamNames(a.bot.Ask().Teams()))))
	}
	if a.task.Trigger().Type == domain.TriggerQuestion {
		return uc.refuse(ctx, a, domain.RefusedDepthExceeded, "this task answers another team's question: it may not ask a third team")
	}
	if a.task.Depth() >= domain.MaxDelegationDepthLimit {
		return uc.refuse(ctx, a, domain.RefusedDepthExceeded, fmt.Sprintf("this task is at depth %d, the deepest a chain may go", a.task.Depth()))
	}
	existing, attempt, err := uc.repeat(ctx, a)
	if err != nil {
		return QuestionAnswer{}, err
	}
	if existing != nil {
		if err := uc.recordAsked(ctx, a, *existing, true); err != nil {
			return QuestionAnswer{}, err
		}
		return QuestionAnswer{TaskID: existing.ID(), BotID: existing.BotID(), Status: existing.Status()}, nil
	}
	inMinute, err := askedInLastMinute(ctx, uc.deps, a.bot.ID(), uc.deps.Clock.Now())
	if err != nil {
		return QuestionAnswer{}, err
	}
	if inMinute >= a.bot.Ask().PerMinute() {
		return uc.refuse(ctx, a, domain.RefusedRateLimited, fmt.Sprintf("bot %s asked %d questions in the last minute, its most; wait before the next", a.bot.ID(), inMinute))
	}
	roster, err := readQuestionRoster(ctx, uc.deps, a.bot.TenantID())
	if err != nil {
		return QuestionAnswer{}, err
	}
	if open := roster.asked[a.bot.ID()]; open >= a.bot.Ask().MaxOpen() {
		return uc.refuse(ctx, a, domain.RefusedRateLimited, fmt.Sprintf("bot %s has %d questions waiting for an answer, its most; wait for their answers", a.bot.ID(), open))
	}
	if traced, err := questionsOfTrace(ctx, uc.deps, a.task.CorrelationID()); err != nil {
		return QuestionAnswer{}, err
	} else if traced >= QuestionsPerTrace {
		return uc.refuse(ctx, a, domain.RefusedRateLimited, fmt.Sprintf("this trace has asked %d questions, the most one trace may ask", traced))
	}
	if queued := roster.queued[a.team]; queued >= QuestionQueuePerTeam {
		return uc.refuse(ctx, a, domain.RefusedTeamBusy, fmt.Sprintf("team %s has %d questions waiting, the most it may queue; ask later", a.team, queued))
	}
	responder, ok := roster.pick(roster.candidates(a.team, a.from))
	if !ok {
		return uc.refuse(ctx, a, domain.RefusedTeamUnavailable, fmt.Sprintf("no member of team %s may answer team %s now: none consents to it, is enabled and has an online node that shares the team's memory", a.team, a.from))
	}
	child, err := uc.create(ctx, a, responder, attempt)
	if err != nil {
		return QuestionAnswer{}, err
	}
	if err := uc.recordAsked(ctx, a, child, false); err != nil {
		return QuestionAnswer{}, err
	}
	return QuestionAnswer{TaskID: child.ID(), BotID: responder.ID(), Status: child.Status(), Created: true}, nil
}

// load finds the execution the node holds and the task and bot behind it.
func (uc AskTeam) load(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command AskTeamCommand) (asking, error) {
	execution, err := runningExecutionOf(ctx, uc.deps, node, executionID, "ask a team")
	if err != nil {
		return asking{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return asking{}, err
	}
	if task.Status() != domain.TaskRunning {
		return asking{}, domain.StateError{Subject: "task " + task.ID().String(), Current: task.Status().String(), Attempted: "ask a team"}
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return asking{}, err
	}
	question, err := domain.NewQuestion(command.Question)
	if err != nil {
		return asking{}, err
	}
	// A team name that is no identifier is no team the bot may ask: the
	// model reads it as a refusal, not as the node's error.
	team, _ := domain.NewTeamName(strings.TrimSpace(command.Team))
	return asking{node: node, execution: execution, task: task, bot: bot, from: memoryTeamOf(bot), team: team, rawTeam: command.Team, question: question, wait: command.Wait}, nil
}

// repeat finds the task the same ask made before, among the asking
// task's questions: nil when there is none, or when the last one ended
// without an answer (failed, expired or cancelled), and then which
// attempt the next one is.
func (uc AskTeam) repeat(ctx context.Context, a asking) (*domain.Task, int, error) {
	base, err := questionKey(a.task.ID(), a.team, a.question, 1)
	if err != nil {
		return nil, 0, err
	}
	attempts := 0
	var last *domain.Task
	for _, id := range a.task.Questions() {
		child, err := uc.deps.Tasks.Get(ctx, id)
		if domain.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		key := child.IdempotencyKey().String()
		if key != base.String() && !strings.HasPrefix(key, base.String()+":") {
			continue
		}
		attempts++
		last = &child
	}
	if last != nil && (last.Status() == domain.TaskCompleted || !last.Status().IsTerminal()) {
		return last, 0, nil
	}
	return nil, attempts + 1, nil
}

// create submits the question's task for the responder and records it on
// the asking task.
func (uc AskTeam) create(ctx context.Context, a asking, responder domain.Bot, attempt int) (domain.Task, error) {
	key, err := questionKey(a.task.ID(), a.team, a.question, attempt)
	if err != nil {
		return domain.Task{}, err
	}
	nonce, err := questionNonce()
	if err != nil {
		return domain.Task{}, err
	}
	objective, err := domain.QuestionFraming{Team: a.from, From: a.bot.ID(), Self: a.team, Question: a.question, Nonce: nonce}.Objective()
	if err != nil {
		return domain.Task{}, err
	}
	now := uc.deps.Clock.Now()
	parent, _ := a.task.Lineage()
	// The hop to another team is counted on its own: it takes nothing of
	// the chain's depth limit, and the question's task can go no deeper.
	depth := a.task.Depth() + 1
	lineage := domain.TaskLineage{
		ParentTaskID: a.task.ID().String(), ParentExecutionID: a.execution.ID().String(), ParentBotID: a.bot.ID().String(),
		Depth: depth, MaxDepth: depth, Chain: append(parent.Chain, a.bot.ID().String()), Grant: questionGrant(responder, now),
	}
	input, _ := json.Marshal(map[string]any{
		"question": a.question.String(), "fromTeam": a.from.String(), "fromBot": a.bot.ID().String(),
		"parentTaskId": a.task.ID().String(), "parentExecutionId": a.execution.ID().String(), "wait": a.wait,
	})
	created, err := submitTask(ctx, uc.deps, submission{
		action: "task.question", bot: responder, trigger: domain.TriggerRef{Type: domain.TriggerQuestion, Name: a.from.String()},
		objective: objective, key: key, correlation: a.task.CorrelationID(), input: input, lineage: &lineage,
	})
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.task.AdoptQuestion(created.Task.ID(), now); err != nil {
		return domain.Task{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, a.task); err != nil {
		return domain.Task{}, err
	}
	return created.Task, nil
}

func (uc AskTeam) recordAsked(ctx context.Context, a asking, child domain.Task, deduplicated bool) error {
	payload := map[string]any{
		"team": a.team.String(), "question": a.question.String(), "wait": a.wait,
		"childTaskId": child.ID().String(), "responder": child.BotID().String(), "deduplicated": deduplicated,
	}
	if err := uc.rec.executionEvent(ctx, domain.EventQuestionAsked, a.bot, a.task.ID(), a.execution.ID(), a.task.CorrelationID(), payload); err != nil {
		return err
	}
	details := map[string]string{"node": a.node.String(), "execution": a.execution.ID().String(), "team": a.team.String(), "child": child.ID().String(), "responder": child.BotID().String(), "deduplicated": strconv.FormatBool(deduplicated)}
	return uc.rec.audit(ctx, "question.ask", a.task.ID().String(), domain.AuditOK, details)
}

// refuse records the refusal and answers it.
func (uc AskTeam) refuse(ctx context.Context, a asking, reason, message string) (QuestionAnswer, error) {
	refusal := domain.QuestionRefusal{Reason: reason, Message: message}
	payload := map[string]any{"team": a.rawTeam, "reason": reason, "message": message, "question": a.question.String()}
	if err := uc.rec.executionEvent(ctx, domain.EventQuestionRefused, a.bot, a.task.ID(), a.execution.ID(), a.task.CorrelationID(), payload); err != nil {
		return QuestionAnswer{}, err
	}
	details := map[string]string{"node": a.node.String(), "execution": a.execution.ID().String(), "team": a.rawTeam, "reason": reason}
	if err := uc.rec.audit(ctx, "question.ask", a.task.ID().String(), domain.AuditDenied, details); err != nil {
		return QuestionAnswer{}, err
	}
	return QuestionAnswer{Refused: &refusal}, nil
}

// teamNames returns the names as plain strings.
func teamNames(names []domain.TeamName) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = name.String()
	}
	return out
}
