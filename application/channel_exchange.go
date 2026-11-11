package application

import (
	"encoding/json"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// channelExchange is a question's task that ended, as the team channel
// tells both sides: the bot that answered, the question's task and the
// execution it ended with (none when it never ran). The answer that goes
// back to the asker and the record each side's node writes into its
// team's store (docs/architecture/team-channel.md, "Memory") are read
// here, once, so the two always agree.
type channelExchange struct {
	responder domain.Bot
	question  domain.Task
	execution domain.Execution
}

// outcome is whether the question was answered and the answer, or the
// reason there is none: the task's, else the execution's.
func (x channelExchange) outcome() (bool, string) {
	if x.question.Status() == domain.TaskCompleted {
		return true, x.execution.Result().Answer
	}
	if reason := x.question.FailureReason(); reason != "" {
		return false, reason
	}
	return false, x.execution.FailureReason()
}

// record is what one side's store keeps of the exchange: the asking team
// (the question trigger's name) on the asked side, the answering team
// (the responder's memory team) on the answered side. It fails for a task
// that is no question that ended, or one whose input lost the question.
func (x channelExchange) record(side domain.ChannelSide) (domain.ChannelRecord, error) {
	if x.question.Trigger().Type != domain.TriggerQuestion {
		return domain.ChannelRecord{}, domain.ValidationError{Field: "channel record", Reason: "the task is not a question"}
	}
	lineage, _ := x.question.Lineage()
	var input struct {
		Question string `json:"question"`
	}
	_ = json.Unmarshal(x.question.Input(), &input)
	asking, answering := domain.TeamName(x.question.Trigger().Name), memoryTeamOf(x.responder)
	team, other := answering, asking
	if side == domain.ChannelAsked {
		team, other = asking, answering
	}
	answered, text := x.outcome()
	answer := []rune(strings.TrimSpace(text))
	if len(answer) > domain.MaxAnswerLength {
		answer = answer[:domain.MaxAnswerLength]
	}
	record := domain.ChannelRecord{
		Side: side, Team: team, Other: other, Question: x.question.ID(), Asker: domain.BotID(lineage.ParentBotID), Responder: x.question.BotID(),
		Text: strings.TrimSpace(input.Question), Answered: answered, Answer: string(answer), Status: x.question.Status(),
	}
	if err := record.Validate(); err != nil {
		return domain.ChannelRecord{}, err
	}
	return record, nil
}

// channelRecordData is a record as an event's payload keeps it: plain
// JSON data, the same in every event store.
func channelRecordData(record domain.ChannelRecord) map[string]any {
	raw, _ := json.Marshal(record.Snapshot())
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	return data
}

// channelRecordFrom reads a record back from an event's payload or a
// task's input; nil when there is none or it does not validate.
func channelRecordFrom(value any) *domain.ChannelRecord {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot domain.ChannelRecordSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil
	}
	record, err := domain.ChannelRecordFromSnapshot(snapshot)
	if err != nil {
		return nil
	}
	return &record
}

// answerMemory is the asking team's record a question_answer task carries
// in its input, for its node to write; none for any other task.
func answerMemory(task domain.Task) *domain.ChannelRecord {
	if task.Trigger().Type != domain.TriggerQuestionAnswer {
		return nil
	}
	var input struct {
		Memory json.RawMessage `json:"memory"`
	}
	if json.Unmarshal(task.Input(), &input) != nil || len(input.Memory) == 0 {
		return nil
	}
	return channelRecordFrom(input.Memory)
}
