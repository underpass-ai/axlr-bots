package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// The states of a CloudWatch alarm, as its notification names the new one.
const (
	CloudWatchAlarm            = "ALARM"
	CloudWatchOK               = "OK"
	CloudWatchInsufficientData = "INSUFFICIENT_DATA"
	// CloudWatchGroupPrefix starts the group key of an alarm's incident.
	CloudWatchGroupPrefix = "cloudwatch:"
)

// CloudWatchAlarmChange is the JSON CloudWatch publishes to an SNS topic
// when an alarm changes state: the SNS notification's Message. Its other
// fields (Trigger, the actions) are kept in Raw, the task's input.
type CloudWatchAlarmChange struct {
	AlarmName        string `json:"AlarmName"`
	AlarmDescription string `json:"AlarmDescription"`
	AWSAccountID     string `json:"AWSAccountId"`
	NewStateValue    string `json:"NewStateValue"`
	NewStateReason   string `json:"NewStateReason"`
	StateChangeTime  string `json:"StateChangeTime"`
	Region           string `json:"Region"`
	AlarmArn         string `json:"AlarmArn"`
	OldStateValue    string `json:"OldStateValue"`
	// Raw is the message as CloudWatch wrote it.
	Raw json.RawMessage `json:"-"`
}

// ParseCloudWatchAlarmChange reads an SNS notification's message as a
// CloudWatch alarm's state change: a JSON object with the alarm's name,
// its new state (ALARM, OK or INSUFFICIENT_DATA) and when it changed.
func ParseCloudWatchAlarmChange(message string) (CloudWatchAlarmChange, error) {
	var change CloudWatchAlarmChange
	if err := json.Unmarshal([]byte(message), &change); err != nil {
		return CloudWatchAlarmChange{}, invalid("Message", "is not a CloudWatch alarm's state change: "+err.Error())
	}
	switch {
	case strings.TrimSpace(change.AlarmName) == "":
		return CloudWatchAlarmChange{}, invalid("Message", "names no AlarmName")
	case strings.TrimSpace(change.StateChangeTime) == "":
		return CloudWatchAlarmChange{}, invalid("Message", "names no StateChangeTime")
	case len(strings.TrimSpace(change.StateChangeTime)) > maxIdentifierLength*2:
		return CloudWatchAlarmChange{}, invalid("Message", "StateChangeTime is too long")
	}
	switch change.NewStateValue {
	case CloudWatchAlarm, CloudWatchOK, CloudWatchInsufficientData:
	default:
		return CloudWatchAlarmChange{}, invalid("Message", "NewStateValue must be ALARM, OK or INSUFFICIENT_DATA")
	}
	change.Raw = json.RawMessage(message)
	return change, nil
}

// GroupKey is the incident's group: cloudwatch:<AlarmArn>, or
// cloudwatch:<AlarmName> when the notification has no ARN. A key past
// the incident's limit (an alarm name may be 255 characters) is the
// SHA-256 of the alarm's name instead, so a real alarm is never refused.
func (c CloudWatchAlarmChange) GroupKey() string {
	id := strings.TrimSpace(c.AlarmArn)
	if id == "" {
		id = strings.TrimSpace(c.AlarmName)
	}
	key := CloudWatchGroupPrefix + id
	if len(key) <= maxIdentifierLength*4 && !hasControl(key) {
		return key
	}
	sum := sha256.Sum256([]byte(id))
	return CloudWatchGroupPrefix + "sha256:" + hex.EncodeToString(sum[:])
}

// Fingerprint is the state change's time: a new ALARM of the same alarm
// is a new alert of its incident, a resend of the same one is not.
func (c CloudWatchAlarmChange) Fingerprint() string { return strings.TrimSpace(c.StateChangeTime) }

// Title is "<AlarmName>: <NewStateReason>", cut to fit an incident's title.
func (c CloudWatchAlarmChange) Title() string {
	title := strings.TrimSpace(c.AlarmName)
	if reason := strings.TrimSpace(c.NewStateReason); reason != "" {
		title += ": " + reason
	}
	return IncidentText(strings.ReplaceAll(title, "\n", " "), maxIncidentTitleLength)
}

// Fields is what an objective template renders over: alarmName, reason,
// state, region, alarmArn and stateChangeTime at the top, and the whole
// message under alarm.
func (c CloudWatchAlarmChange) Fields() map[string]any {
	var alarm map[string]any
	_ = json.Unmarshal(c.Raw, &alarm)
	return map[string]any{
		"alarmName": c.AlarmName, "reason": c.NewStateReason, "state": c.NewStateValue, "region": c.Region,
		"alarmArn": c.AlarmArn, "stateChangeTime": c.StateChangeTime, "alarm": alarm,
	}
}

func hasControl(raw string) bool {
	for _, r := range raw {
		if r < 0x20 {
			return true
		}
	}
	return false
}
