package application

import (
	"context"
	"fmt"

	"github.com/underpass-ai/axlr-bots/domain"
)

// CloudWatchSource is the source of an incident a cloudwatch trigger
// opened.
const CloudWatchSource = "cloudwatch"

// fireCloudWatch takes what Amazon SNS posts to the bot's cloudwatch
// trigger. The message's signature is verified first, whatever route it
// came by: SNS sets no header of ours, so its own signature is the only
// credential (decided 8 November 2026). A message of a topic the trigger
// does not list, or stamped more than 15 minutes from now (a replay), is
// refused. Then, by the message's type:
//
//   - SubscriptionConfirmation: audited with its SubscribeURL; the
//     subscription is confirmed (the URL visited) only when the trigger
//     says autoConfirm, otherwise a person confirms it by hand.
//   - UnsubscribeConfirmation: audited, nothing else.
//   - Notification: the CloudWatch alarm's state change in its Message.
//     ALARM opens, repeats or reopens the alarm's incident and its triage
//     task, as a firing Alertmanager group does; OK resolves it and starts
//     nothing; INSUFFICIENT_DATA is audited and ignored.
//
// Every fire is a bot.trigger.fired event that says what it did.
func (uc FireTrigger) fireCloudWatch(ctx context.Context, bot domain.Bot, trigger domain.Trigger, command FireTriggerCommand) (FireTriggerResult, error) {
	deny := func(err error) (FireTriggerResult, error) {
		_ = uc.rec.audit(ctx, "trigger.fire", bot.ID().String(), domain.AuditDenied, map[string]string{"trigger": trigger.Name(), "reason": err.Error()})
		return FireTriggerResult{}, err
	}
	message, err := domain.ParseSNSMessage(command.Body)
	if err != nil {
		return deny(err)
	}
	if command.MessageType != "" && command.MessageType != message.Type {
		return deny(domain.ValidationError{Field: "x-amz-sns-message-type", Reason: "says " + command.MessageType + " and the body's Type " + message.Type})
	}
	// Checked before the certificate is fetched, so a refused message
	// costs no fetch; the topic and the Timestamp are signed, so a forger
	// gains nothing by changing them, and Verify below refuses it.
	if !trigger.AllowsTopic(message.TopicArn) {
		return deny(domain.ForbiddenError{Reason: "the SNS topic " + message.TopicArn + " is not among the trigger's topics"})
	}
	if err := message.CheckFresh(uc.deps.Clock.Now()); err != nil {
		return deny(err)
	}
	certificate, err := uc.deps.SNS.SigningCertificate(ctx, message.SigningCertURL)
	if err != nil {
		// Not a refusal: SNS retries a delivery that fails on our side.
		_ = uc.rec.audit(ctx, "trigger.fire", bot.ID().String(), domain.AuditFailed, map[string]string{"trigger": trigger.Name(), "reason": "signing certificate: " + err.Error()})
		return FireTriggerResult{}, fmt.Errorf("the SNS signing certificate could not be fetched: %w", err)
	}
	if err := message.Verify(certificate); err != nil {
		return deny(err)
	}
	fired := map[string]any{"trigger": trigger.Name(), "type": trigger.Type().String(), "messageType": message.Type, "messageId": message.MessageID, "topicArn": message.TopicArn}
	result := FireTriggerResult{Trigger: trigger.Ref(), Action: IncidentIgnored}
	switch message.Type {
	case domain.SNSSubscriptionConfirmation:
		if result.Action, err = uc.subscription(ctx, bot, trigger, message); err != nil {
			return FireTriggerResult{}, err
		}
	case domain.SNSUnsubscribeConfirmation:
		details := map[string]string{"trigger": trigger.Name(), "topicArn": message.TopicArn, "messageId": message.MessageID}
		if err := uc.rec.audit(ctx, "trigger.unsubscription", bot.ID().String(), domain.AuditOK, details); err != nil {
			return FireTriggerResult{}, err
		}
	default:
		change, err := domain.ParseCloudWatchAlarmChange(message.Message)
		if err != nil {
			return deny(err)
		}
		fired["alarmName"], fired["state"], fired["groupKey"], fired["stateChangeTime"] = change.AlarmName, change.NewStateValue, change.GroupKey(), change.Fingerprint()
		if result, err = uc.alarmChange(ctx, bot, trigger, change, result); err != nil {
			return FireTriggerResult{}, err
		}
	}
	return result, uc.recordIncidentFire(ctx, bot, result, fired)
}

// subscription audits a subscription's confirmation with the URL that
// confirms it, and visits it when the trigger confirms by itself. By
// default a person does: any AWS account can subscribe this endpoint to
// a topic of its own, and confirming it would let that topic open
// incidents here.
func (uc FireTrigger) subscription(ctx context.Context, bot domain.Bot, trigger domain.Trigger, message domain.SNSMessage) (string, error) {
	details := map[string]string{"trigger": trigger.Name(), "topicArn": message.TopicArn, "subscribeUrl": message.SubscribeURL, "autoConfirm": fmt.Sprint(trigger.AutoConfirm())}
	if err := uc.rec.audit(ctx, "trigger.subscription", bot.ID().String(), domain.AuditOK, details); err != nil {
		return "", err
	}
	if !trigger.AutoConfirm() {
		return IncidentIgnored, nil
	}
	if err := uc.deps.SNS.ConfirmSubscription(ctx, message.SubscribeURL); err != nil {
		_ = uc.rec.audit(ctx, "trigger.subscription.confirm", bot.ID().String(), domain.AuditFailed, map[string]string{"trigger": trigger.Name(), "topicArn": message.TopicArn, "reason": err.Error()})
		return "", fmt.Errorf("the SNS subscription could not be confirmed: %w", err)
	}
	if err := uc.rec.audit(ctx, "trigger.subscription.confirm", bot.ID().String(), domain.AuditOK, map[string]string{"trigger": trigger.Name(), "topicArn": message.TopicArn}); err != nil {
		return "", err
	}
	return IncidentConfirmed, nil
}

// alarmChange maps an alarm's new state to its incident: ALARM opens,
// repeats or reopens it (the alarm's JSON is the triage task's input),
// OK resolves it, INSUFFICIENT_DATA changes nothing.
func (uc FireTrigger) alarmChange(ctx context.Context, bot domain.Bot, trigger domain.Trigger, change domain.CloudWatchAlarmChange, result FireTriggerResult) (FireTriggerResult, error) {
	switch change.NewStateValue {
	case domain.CloudWatchAlarm:
		opened, err := NewOpenIncident(uc.deps).open(ctx, opening{
			bot: bot, trigger: trigger, source: CloudWatchSource, groupKey: change.GroupKey(), title: change.Title(), severity: trigger.Severity(),
			fingerprints: []string{change.Fingerprint()}, input: change.Raw, fields: change.Fields(),
		})
		if err != nil {
			return FireTriggerResult{}, err
		}
		incident := opened.Incident
		result.Incident, result.Task, result.Created, result.Action = &incident, opened.Task, opened.Created, opened.Action
		if opened.Task != nil {
			result.IdempotencyKey = opened.Task.IdempotencyKey()
		}
	case domain.CloudWatchOK:
		incident, err := uc.resolveGroup(ctx, bot, trigger, change.GroupKey(), nil)
		if err != nil {
			return FireTriggerResult{}, err
		}
		if incident != nil {
			result.Incident, result.Action = incident, IncidentClosed
		}
	default:
		details := map[string]string{"trigger": trigger.Name(), "groupKey": change.GroupKey(), "ignored": "the alarm's state is " + change.NewStateValue}
		if err := uc.rec.audit(ctx, "trigger.fire", bot.ID().String(), domain.AuditOK, details); err != nil {
			return FireTriggerResult{}, err
		}
	}
	return result, nil
}

// recordIncidentFire publishes the bot.trigger.fired event of a fire that
// acts on an incident: what it did, the incident and the task it started.
func (uc FireTrigger) recordIncidentFire(ctx context.Context, bot domain.Bot, result FireTriggerResult, fired map[string]any) error {
	fired["action"], fired["deduplicated"] = result.Action, result.Action == IncidentRepeated
	var task domain.TaskID
	var correlation domain.CorrelationID
	if result.Incident != nil {
		fired["incidentId"] = result.Incident.ID().String()
	}
	if result.Task != nil {
		task, correlation = result.Task.ID(), result.Task.CorrelationID()
		fired["taskId"], fired["idempotencyKey"], fired["created"] = task.String(), result.IdempotencyKey.String(), result.Created
	}
	return uc.rec.event(ctx, domain.EventTriggerFired, bot, task, correlation, fired)
}
