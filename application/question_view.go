package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// QuestionView is where a question stands, read from the control plane's
// own records only: the task, the bot that answers it and its other open
// tasks, the node it waits for, the run's last step, a wait for a person,
// and the deadline. It never holds the answer: that comes back through the
// channel. The asker reads it with axlr_bots_question_status; a team's
// Jev watcher reads it as the state it decides on
// (docs/architecture/team-channel.md, A6).
type QuestionView struct {
	Question  domain.TaskID
	Team      domain.TeamName
	AskedBy   domain.BotID
	Responder domain.BotID
	Status    domain.TaskStatus
	AskedAt   time.Time
	Deadline  time.Time
	Now       time.Time
	// OpenTasks are the responder's other tasks not yet ended.
	OpenTasks int
	// Waiting says why a Pending question has not started.
	Waiting string
	// RunningSince is when its run began; zero when it is not running.
	RunningSince time.Time
	// LastStep is the run's newest step, and when.
	LastStep   string
	LastStepAt time.Time
	// ApprovalTool is the call that waits for a person, since when.
	ApprovalTool  string
	ApprovalSince time.Time
	// Reason is why it ended without an answer.
	Reason string
}

// Text is the view in a few lines, for a judge or a person.
func (v QuestionView) Text() string {
	age := func(at time.Time) string { return v.Now.Sub(at).Truncate(time.Minute).String() }
	var b strings.Builder
	fmt.Fprintf(&b, "Question %s from %s to team %s, asked %s ago; status %s.", v.Question, v.AskedBy, v.Team, age(v.AskedAt), v.Status)
	fmt.Fprintf(&b, "\nResponder %s has %d other open tasks.", v.Responder, v.OpenTasks)
	if v.Waiting != "" {
		fmt.Fprintf(&b, "\nIt waits: %s.", v.Waiting)
	}
	if !v.RunningSince.IsZero() {
		fmt.Fprintf(&b, "\nRunning for %s.", age(v.RunningSince))
	}
	if v.LastStep != "" {
		fmt.Fprintf(&b, "\nLast step %s ago: %s.", age(v.LastStepAt), v.LastStep)
	}
	if v.ApprovalTool != "" {
		fmt.Fprintf(&b, "\nWaiting %s for a person to approve %s.", age(v.ApprovalSince), v.ApprovalTool)
	}
	if v.Reason != "" {
		fmt.Fprintf(&b, "\nEnded without an answer: %s.", v.Reason)
	}
	if !v.Status.IsTerminal() {
		fmt.Fprintf(&b, "\nDeadline in %s.", v.Deadline.Sub(v.Now).Truncate(time.Minute))
	}
	return b.String()
}

// readQuestionView builds the view of a question's task.
func readQuestionView(ctx context.Context, deps Dependencies, question domain.Task) (QuestionView, error) {
	lineage, ok := question.Lineage()
	if !ok || question.Trigger().Type != domain.TriggerQuestion {
		return QuestionView{}, domain.NotFoundError{Kind: "question", ID: question.ID().String()}
	}
	v := QuestionView{
		Question: question.ID(), AskedBy: domain.BotID(lineage.ParentBotID), Responder: question.BotID(), Status: question.Status(),
		AskedAt: question.CreatedAt(), Deadline: lineage.Grant.Deadline, Now: deps.Clock.Now(),
	}
	responder, err := deps.Bots.Get(ctx, question.BotID())
	switch {
	case domain.IsNotFound(err):
		v.Waiting = "its responder is gone"
	case err != nil:
		return QuestionView{}, err
	default:
		v.Team = memoryTeamOf(responder)
	}
	if question.Status().IsTerminal() {
		if question.Status() != domain.TaskCompleted {
			v.Reason = question.FailureReason()
		}
		return v, nil
	}
	if v.OpenTasks, err = openTasksBesides(ctx, deps, question); err != nil {
		return QuestionView{}, err
	}
	switch question.Status() {
	case domain.TaskPending:
		if v.Waiting == "" {
			if v.Waiting, err = pendingReason(ctx, deps, responder, question, v.OpenTasks); err != nil {
				return QuestionView{}, err
			}
		}
	case domain.TaskRunning:
		if current := question.CurrentExecutionID(); current != "" {
			execution, err := deps.Executions.Get(ctx, current)
			if err != nil {
				return QuestionView{}, err
			}
			v.RunningSince = execution.StartedAt()
		}
		if v.LastStep, v.LastStepAt, err = lastStep(ctx, deps, question.ID()); err != nil {
			return QuestionView{}, err
		}
	case domain.TaskWaitingApproval:
		if wait, ok := question.Approval(); ok && wait.Open() {
			v.ApprovalTool, v.ApprovalSince = wait.Tool, wait.Since
		}
	}
	return v, nil
}

// openTasksBesides counts the responder's tasks not yet ended, the
// question's own left out.
func openTasksBesides(ctx context.Context, deps Dependencies, question domain.Task) (int, error) {
	count := 0
	for _, status := range []domain.TaskStatus{domain.TaskPending, domain.TaskRunning, domain.TaskWaitingApproval} {
		tasks, err := deps.Tasks.ListByStatus(ctx, status)
		if err != nil {
			return 0, err
		}
		for _, task := range tasks {
			if task.BotID() == question.BotID() && task.ID() != question.ID() {
				count++
			}
		}
	}
	return count, nil
}

// pendingReason says why a Pending question has not started: no node that
// opens the team's store is Online, its placement admits none, or its
// responder is busy.
func pendingReason(ctx context.Context, deps Dependencies, responder domain.Bot, question domain.Task, busy int) (string, error) {
	if !responder.AcceptsTasks() {
		return "its responder is " + responder.Status().String(), nil
	}
	nodes, err := deps.Nodes.List(ctx)
	if err != nil {
		return "", err
	}
	if waiting, ok := placementWait(responder, question.Skills(), nodes); ok {
		return waiting, nil
	}
	sharing := false
	for _, node := range nodes {
		sharing = sharing || answersOn(responder, node)
	}
	if !sharing {
		return "no Online node that shares team " + memoryTeamOf(responder).String() + "'s memory", nil
	}
	if busy > 0 {
		return fmt.Sprintf("its responder is busy with %d other tasks", busy), nil
	}
	return "its turn on the node has not come", nil
}

// lastStep is the newest step the run reported, in a few words.
func lastStep(ctx context.Context, deps Dependencies, task domain.TaskID) (string, time.Time, error) {
	events, err := deps.EventReader.ListTaskEvents(ctx, task, MaxEventLimit)
	if err != nil {
		return "", time.Time{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != domain.EventTaskProgress {
			continue
		}
		items, _ := events[i].Payload["items"].([]any)
		if len(items) == 0 {
			continue
		}
		item, _ := items[len(items)-1].(map[string]any)
		words := []string{}
		for _, key := range []string{"kind", "tool", "mcpTool", "status"} {
			if value, _ := item[key].(string); value != "" {
				words = append(words, value)
			}
		}
		return strings.Join(words, " "), events[i].CreatedAt, nil
	}
	return "", time.Time{}, nil
}
