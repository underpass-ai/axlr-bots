package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Limits of the team channel the control plane holds, whatever a bot
// says (docs/architecture/team-channel.md).
const (
	// QuestionModelCalls caps the model calls of a question's task.
	QuestionModelCalls = 6
	// QuestionToolCalls caps its tool calls.
	QuestionToolCalls = 12
	// QuestionTimeout caps how long it may take, from when it is asked.
	QuestionTimeout = 300 * time.Second
	// QuestionsPerTrace is the most questions one trace may ask.
	QuestionsPerTrace = 3
	// QuestionQueuePerTeam is the most questions that may wait Pending
	// for one team.
	QuestionQueuePerTeam = 8
	// questionRateWindow is the window of a bot's ask.perMinute.
	questionRateWindow = time.Minute
)

// questionKey is the question's idempotency key: the asking task and
// what it asked of whom. The responder is not known before the pick, so
// the repeat is found among the asking task's questions; attempt counts
// the questions it asked again after one failed or expired.
func questionKey(parent domain.TaskID, team domain.TeamName, question domain.Question, attempt int) (domain.IdempotencyKey, error) {
	sum := sha256.Sum256([]byte(team.String() + "\n" + question.String()))
	key := "question:" + parent.String() + ":" + hex.EncodeToString(sum[:12])
	if attempt > 1 {
		key += ":" + strconv.Itoa(attempt)
	}
	return domain.NewIdempotencyKey(key)
}

// questionGrant is what a question's task runs with: its own bot's
// budgets, capped by the channel's limits, until a deadline from now.
func questionGrant(responder domain.Bot, now time.Time) domain.DelegationGrant {
	own := responder.Budgets()
	model, tools := own.MaxModelCallsPerTask(), own.MaxToolCallsPerTask()
	if model == 0 || model > QuestionModelCalls {
		model = QuestionModelCalls
	}
	if tools == 0 || tools > QuestionToolCalls {
		tools = QuestionToolCalls
	}
	span := own.TaskTimeoutOr(DefaultTaskTimeout)
	if span > QuestionTimeout {
		span = QuestionTimeout
	}
	return domain.DelegationGrant{MaxModelCalls: model, MaxToolCalls: tools, Deadline: now.Add(span)}
}

// questionNonce draws the nonce that closes a question's block in its
// objective: fresh for each question, so the question cannot close it.
func questionNonce() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// askedInLastMinute counts what the bot asked in the last minute, from
// its bot.question.asked events: a repeat is not asking again.
func askedInLastMinute(ctx context.Context, deps Dependencies, bot domain.BotID, now time.Time) (int, error) {
	events, err := deps.EventReader.ListEvents(ctx, bot, eventHistory)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, event := range events {
		if event.Type != domain.EventQuestionAsked || now.Sub(event.CreatedAt) >= questionRateWindow {
			continue
		}
		if repeat, _ := event.Payload["deduplicated"].(bool); repeat {
			continue
		}
		child, _ := event.Payload["childTaskId"].(string)
		seen[child] = true
	}
	return len(seen), nil
}

// questionsOfTrace counts the questions the trace has asked.
func questionsOfTrace(ctx context.Context, deps Dependencies, trace domain.CorrelationID) (int, error) {
	tasks, err := deps.Tasks.List(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, task := range tasks {
		if task.Trigger().Type == domain.TriggerQuestion && task.CorrelationID() == trace {
			count++
		}
	}
	return count, nil
}
