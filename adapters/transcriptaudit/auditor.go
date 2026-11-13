package transcriptaudit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The judge's options and the question it answers for every block (the
// wording of the spike of 13 November 2026, 9 of 12 hidden failures, 1 of
// 12 clean tasks flagged: artifacts/auditor-jev-spike).
const (
	OptionClean       = "clean"
	OptionHiddenError = "hidden_error"
	verdictSkipped    = "skipped"
	excerptBytes      = 400
	listPage          = 100
	question          = "This block is part of the full transcript of one task an automated bot ran; the run itself was reported " +
		"as completed. Does this block show a hidden failure: a tool or program that refused, errored or could not run, " +
		"a check that was skipped, omitted or not really done (even if the bot mentions it), missing data, files or setup, " +
		"or a result the bot reported as fine or 'none' although something went wrong? Text the bot only read from a " +
		"playbook or a document is instructions, not something that happened. A daily cap reached, a test that fails as " +
		"designed, or nothing to do are not hidden failures."
)

// Auditor audits the transcripts after its log's cursor, one at a time,
// in the order they came. It is not safe for concurrent use.
type Auditor struct {
	config AuditorConfig
	teams  map[string]string
}

// NewAuditor checks the configuration.
func NewAuditor(config AuditorConfig) (*Auditor, error) {
	if config.Source == nil || config.Judge == nil || config.Log == nil {
		return nil, errors.New("transcriptaudit: an auditor needs a source, a judge and a verdict log")
	}
	if _, err := domain.NewTeamName(config.Team); err != nil {
		return nil, err
	}
	if _, err := domain.NewEventName(config.Event); err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logger == nil {
		config.Logger = log.New(io.Discard, "", 0)
	}
	return &Auditor{config: config, teams: map[string]string{}}, nil
}

// Round audits every transcript after the cursor and answers how many it
// audited. An error stops the round at the transcript it could not
// finish: the next round starts there again.
func (a *Auditor) Round(ctx context.Context) (int, error) {
	a.loadTeams(ctx)
	done := 0
	for {
		records, err := a.config.Source.ListTranscripts(ctx, a.config.Log.Cursor(), listPage)
		if err != nil {
			return done, err
		}
		if len(records) == 0 {
			return done, nil
		}
		for _, record := range records {
			if err := a.audit(ctx, record); err != nil {
				return done, fmt.Errorf("transcript %d (%s/%s): %w", record.Seq, record.Execution, record.Session, err)
			}
			done++
		}
	}
}

// Run audits a round every interval until ctx ends; once runs one round.
func (a *Auditor) Run(ctx context.Context, interval time.Duration, once bool) error {
	for {
		audited, err := a.Round(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			a.config.Logger.Printf("transcript-audit: %v", err)
		} else if audited > 0 {
			a.config.Logger.Printf("transcript-audit: audited %d, cursor %d", audited, a.config.Log.Cursor())
		}
		if once {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// loadTeams records each bot's first team by name; a failure keeps the
// teams it knew.
func (a *Auditor) loadTeams(ctx context.Context) {
	groups, err := a.config.Source.ListGroups(ctx)
	if err != nil {
		return
	}
	slices.SortFunc(groups, func(x, y application.Group) int {
		if x.Name < y.Name {
			return -1
		}
		if x.Name > y.Name {
			return 1
		}
		return 0
	})
	teams := map[string]string{}
	for _, group := range groups {
		for _, member := range group.Members {
			if _, ok := teams[member.BotID]; !ok {
				teams[member.BotID] = group.Name
			}
		}
	}
	a.teams = teams
}

// audit judges one transcript, publishes it when a block hides a failure,
// and records the verdict.
func (a *Auditor) audit(ctx context.Context, record application.TranscriptRecord) error {
	content, err := a.config.Source.GetTranscript(ctx, record.Execution.String(), record.Session)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(content)
	verdict := VerdictLine{Seq: int(record.Seq), Task: record.Task.String(), Execution: record.Execution.String(), Session: record.Session, Bot: record.Bot.String(), Sha256: hex.EncodeToString(sum[:]), Model: a.config.Model}
	lines, err := sessionLines(content)
	if err != nil {
		verdict.Verdict = verdictSkipped
		verdict.AuditedAt = a.config.Now().UTC().Format(time.RFC3339)
		a.config.Logger.Printf("transcript-audit: %d skipped: %v", record.Seq, err)
		return a.config.Log.Append(verdict)
	}
	header := fmt.Sprintf("Task %s of bot %s, execution %s.", record.Task, record.Bot, record.Execution)
	cut := blocks(lines, MaxStateBytes-len(header)-64)
	verdict.Blocks = len(cut)
	verdict.Flagged = []FlaggedBlock{}
	for i, block := range cut {
		state := fmt.Sprintf("%s Block %d of %d.\n%s", header, i+1, len(cut), block)
		judgement, err := a.config.Judge.Choose(ctx, Choice{State: state, Question: question, Options: []string{OptionClean, OptionHiddenError}})
		if err != nil {
			return err
		}
		verdict.InputTokens += judgement.InputTokens
		if judgement.Choice == OptionHiddenError {
			verdict.Flagged = append(verdict.Flagged, FlaggedBlock{Block: i + 1, Probability: judgement.Probability, Excerpt: cutBytes(block, excerptBytes), Evidence: evidence(block)})
		}
	}
	verdict.Verdict = OptionClean
	if len(verdict.Flagged) > 0 {
		verdict.Verdict = OptionHiddenError
		id, err := a.publish(ctx, record, verdict)
		if err != nil {
			return err
		}
		verdict.EventID = id
	}
	verdict.AuditedAt = a.config.Now().UTC().Format(time.RFC3339)
	return a.config.Log.Append(verdict)
}

// publish tells the team; a dry run answers the empty id.
func (a *Auditor) publish(ctx context.Context, record application.TranscriptRecord, verdict VerdictLine) (string, error) {
	if a.config.Publisher == nil {
		return "", nil
	}
	team := a.teams[record.Bot.String()]
	shown := team
	if shown == "" {
		shown = "-"
	}
	data := FailureData{Seq: int(record.Seq), Task: verdict.Task, Execution: verdict.Execution, Session: verdict.Session, Bot: verdict.Bot, Team: team, Blocks: verdict.Blocks, Flagged: verdict.Flagged, Model: verdict.Model}
	message := fmt.Sprintf("%s · %s (%s) · %d of %d blocks hidden_error · %s/%s", verdict.Task, verdict.Bot, shown, len(verdict.Flagged), verdict.Blocks, verdict.Execution, verdict.Session)
	command := application.PublishGroupEventCommand{Group: a.config.Team, Name: a.config.Event, Message: message, Data: toMap(data), IdempotencyKey: "transcript:" + strconv.FormatInt(record.Seq, 10)}
	answer, err := a.config.Publisher.PublishGroupEvent(ctx, command)
	if err != nil {
		return "", err
	}
	return answer.EventID, nil
}

// toMap is the data as the publish body's object.
func toMap(data FailureData) map[string]any {
	raw, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var fields map[string]any
	_ = dec.Decode(&fields)
	return fields
}
