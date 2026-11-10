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
	// QuestionTimeout is how long a question has to be answered, from
	// when it is asked, waiting for its responder included.
	QuestionTimeout = 2 * time.Hour
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
// budgets and timeout (a grant of 0 calls leaves the bot's own), and a
// deadline two hours from the ask.
func questionGrant(now time.Time) domain.DelegationGrant {
	return domain.DelegationGrant{Deadline: now.Add(QuestionTimeout)}
}

// channelNonce draws the nonce that closes a question's or an answer's
// block: fresh for each, so the text cannot close it itself.
func channelNonce() (string, error) {
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
