package application

import (
	"strconv"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SlackIncidentMessage is the Block Kit message a slack output posts
// on an incident event: Text is the line a notification shows, Blocks
// the message itself (a header with the severity and the title, the
// facts, the cause, the proposal, the end, the panel button).
type SlackIncidentMessage struct {
	Text   string       `json:"text"`
	Blocks []SlackBlock `json:"blocks"`
}

// SlackBlock is one Block Kit block: a header or a section with Text, a
// section with Fields, or actions with Elements.
type SlackBlock struct {
	Type     string        `json:"type"`
	Text     *SlackText    `json:"text,omitempty"`
	Fields   []SlackText   `json:"fields,omitempty"`
	Elements []SlackButton `json:"elements,omitempty"`
}

// SlackText is a Block Kit text object: plain_text or mrkdwn.
type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SlackButton is a link button: it opens URL, Slack sends nothing back.
type SlackButton struct {
	Type     string    `json:"type"`
	Text     SlackText `json:"text"`
	URL      string    `json:"url"`
	ActionID string    `json:"action_id"`
}

// Limits of Block Kit that the message keeps to.
const (
	slackHeaderLimit  = 150
	slackSectionLimit = 3000
)

// NewSlackIncidentMessage writes the message of an incident event from
// the body a webhook would receive: the same facts, in Block Kit.
func NewSlackIncidentMessage(event IncidentOutputEvent) SlackIncidentMessage {
	title := strings.ToUpper(event.Severity) + " · " + event.Title
	message := SlackIncidentMessage{Text: slackEscape("[" + event.Severity + "] " + event.Title + ": " + slackEventWords(event.Event))}
	message.Blocks = append(message.Blocks,
		SlackBlock{Type: "header", Text: &SlackText{Type: "plain_text", Text: slackCut(title, slackHeaderLimit)}},
		slackSection("*"+slackEscape(slackEventWords(event.Event))+"* · `"+slackEscape(event.IncidentID)+"` · tenant `"+slackEscape(event.TenantID)+"`"),
		SlackBlock{Type: "section", Fields: []SlackText{
			{Type: "mrkdwn", Text: "*Status*\n" + slackEscape(event.Status)},
			{Type: "mrkdwn", Text: "*Bot*\n" + slackEscape(event.BotName)},
			{Type: "mrkdwn", Text: "*Cycle*\n" + strconv.Itoa(event.Cycle)},
			{Type: "mrkdwn", Text: "*Cost*\n" + slackCost(event)},
		}},
	)
	if event.Diagnosis != "" && event.Status != domain.IncidentOpen.String() {
		message.Blocks = append(message.Blocks, slackSection("*Cause*\n"+slackEscape(event.Diagnosis)))
	}
	switch event.Status {
	case domain.IncidentProposed.String():
		if event.Proposal != "" {
			message.Blocks = append(message.Blocks, slackSection("*Proposal*\n```"+slackEscape(event.Proposal)+"```"))
		}
		message.Blocks = append(message.Blocks, slackSection("*Needs you*: approve or deny the change."))
	case domain.IncidentResolved.String():
		line := "*Resolved*"
		if event.DurationSeconds > 0 {
			line += " after " + slackDuration(event.DurationSeconds)
		}
		if event.Reason != "" {
			line += ": " + slackEscape(event.Reason)
		}
		message.Blocks = append(message.Blocks, slackSection(line))
	case domain.IncidentEscalated.String():
		line := "*Escalated*"
		if event.Reason != "" {
			line += ": " + slackEscape(event.Reason)
		}
		message.Blocks = append(message.Blocks, slackSection(line+"\n*Needs you*: the bots gave it to a person."))
	}
	if event.Links.Panel != "" {
		message.Blocks = append(message.Blocks, SlackBlock{Type: "actions", Elements: []SlackButton{{
			Type: "button", Text: SlackText{Type: "plain_text", Text: "Open in the panel"}, URL: event.Links.Panel, ActionID: "open-incident",
		}}})
	}
	return message
}

func slackSection(text string) SlackBlock {
	return SlackBlock{Type: "section", Text: &SlackText{Type: "mrkdwn", Text: slackCut(text, slackSectionLimit)}}
}

// slackEventWords says the event in a few words.
func slackEventWords(event string) string {
	switch domain.EventType(event) {
	case domain.EventIncidentOpened:
		return "Incident opened"
	case domain.EventIncidentRepeated:
		return "Alert repeated"
	case domain.EventIncidentReopened:
		return "Incident reopened"
	case domain.EventIncidentDiagnosed:
		return "Cause found"
	case domain.EventIncidentProposed:
		return "Change proposed"
	case domain.EventIncidentApplying:
		return "Change approved, applying"
	case domain.EventIncidentVerifying:
		return "Change applied, verifying"
	case domain.EventIncidentResolved:
		return "Incident resolved"
	case domain.EventIncidentEscalated:
		return "Incident escalated"
	}
	return event
}

// slackEscape keeps a text from being read as a link, a mention or a
// command: Slack's three control characters, escaped as it asks.
func slackEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// slackCut cuts a text to max characters with a trailing "…".
func slackCut(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max-1]) + "…"
}

func slackCost(event IncidentOutputEvent) string {
	cost := "$" + strconv.FormatFloat(event.CostUSD, 'f', 4, 64)
	if event.CostUnknown {
		cost += " (a run did not report)"
	}
	return cost
}

// slackDuration says seconds as 45s, 12m 30s or 2h 5m.
func slackDuration(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Minute:
		return strconv.FormatInt(seconds, 10) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m " + strconv.Itoa(int(d%time.Minute/time.Second)) + "s"
	}
	return strconv.Itoa(int(d/time.Hour)) + "h " + strconv.Itoa(int(d%time.Hour/time.Minute)) + "m"
}
