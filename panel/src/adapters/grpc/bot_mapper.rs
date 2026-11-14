use serde_json::{Value, json};

use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{Bot, BotReference, DeletedBot, StopRefusal, StoppedBotAnswer};

/// Bots as the control plane's messages carry them, into the panel's.
pub(super) struct BotMapper;

impl BotMapper {
    pub fn bot(message: Option<proto::Bot>) -> Result<Bot, PanelError> {
        let message = message.ok_or_else(|| PanelError::Unreadable("the answer has no bot".into()))?;
        Wire::read(Self::bot_shape(&message), "bot")
    }

    pub fn bots(messages: Vec<proto::Bot>) -> Result<Vec<Bot>, PanelError> {
        messages.iter().map(|m| Wire::read(Self::bot_shape(m), "bot")).collect()
    }

    pub fn deleted(answer: proto::DeleteBotResponse) -> Result<DeletedBot, PanelError> {
        let bot = Self::bot(answer.bot)?;
        let named_by = answer
            .named_by
            .into_iter()
            .map(|r| {
                Wire::read::<BotReference>(
                    json!({"botId": r.bot_id, "how": r.how, "topic": r.topic}),
                    "bot reference",
                )
            })
            .collect::<Result<_, _>>()?;
        Ok(DeletedBot {
            bot,
            cancelled: answer.cancelled,
            children_cancelled: answer.children_cancelled,
            timers_removed: u32::try_from(answer.timers_removed).unwrap_or(0),
            notifications_dropped: u32::try_from(answer.notifications_dropped).unwrap_or(0),
            named_by,
        })
    }

    pub fn stopped(answer: proto::StopBotResponse) -> Result<StoppedBotAnswer, PanelError> {
        Ok(StoppedBotAnswer {
            bot: Self::bot(answer.bot)?,
            cancelled: answer.cancelled,
            not_cancelled: answer
                .not_cancelled
                .into_iter()
                .map(|r| Wire::read::<StopRefusal>(json!({"taskId": r.task_id, "reason": r.reason}), "stop refusal"))
                .collect::<Result<_, _>>()?,
        })
    }

    fn bot_shape(bot: &proto::Bot) -> Value {
        let model = bot.model.clone().unwrap_or_default();
        let budgets = bot.budgets.unwrap_or_default();
        let rules = bot.tool_rules.clone().unwrap_or_default();
        let placement = bot.placement.clone().unwrap_or_default();
        json!({
            "id": bot.id,
            "tenantId": bot.tenant_id,
            "name": bot.name,
            "owner": bot.owner,
            "mission": bot.mission,
            "allowedTools": bot.allowed_tools,
            "model": {
                "model": model.model,
                "routingPolicy": model.routing_policy,
                "allowRemoteProviders": model.allow_remote_providers,
                "maxContextTokens": model.max_context_tokens.max(0),
            },
            "budgets": {
                "maxModelCallsPerTask": budgets.max_model_calls_per_task.max(0),
                "maxToolCallsPerTask": budgets.max_tool_calls_per_task.max(0),
                "maxDelegationDepth": budgets.max_delegation_depth.max(0),
                "taskTimeoutSeconds": Wire::seconds(budgets.task_timeout.as_ref()),
            },
            "level": bot.level,
            "toolRules": {"allow": Self::rules(&rules.allow), "deny": Self::rules(&rules.deny)},
            "approvals": {
                "autoApprove": bot.approvals.as_ref().map(|a| Self::rules(&a.auto_approve)).unwrap_or_default(),
            },
            "delegation": bot.delegation.as_ref().map(|d| json!({"to": d.to})),
            "triggers": bot.triggers.iter().map(Self::trigger).collect::<Vec<_>>(),
            "placement": {"nodes": placement.nodes, "selector": Wire::strings(&placement.selector)},
            "groups": bot.groups,
            "subscriptions": bot.subscriptions.iter().map(Self::subscription).collect::<Vec<_>>(),
            "version": bot.version,
            "staged": bot.staged.as_ref().map(|s| json!({
                "version": s.version,
                "stagedAt": Wire::time(s.staged_at.as_ref()),
            })),
            "status": bot.status,
            "createdAt": Wire::time(bot.created_at.as_ref()),
            "updatedAt": Wire::time(bot.updated_at.as_ref()),
        })
    }

    fn rules(rules: &[proto::ToolRule]) -> Vec<Value> {
        rules
            .iter()
            .map(|r| json!({"tool": r.tool, "arguments": Wire::strings(&r.arguments)}))
            .collect()
    }

    fn trigger(trigger: &proto::Trigger) -> Value {
        json!({
            "type": trigger.r#type,
            "name": trigger.name,
            "objective": trigger.objective,
            "secret": trigger.secret,
            "signingKey": trigger.signing_key,
            "every": Wire::duration_text(trigger.every.as_ref()),
            "cron": trigger.cron,
            "events": trigger.events,
            "interrupt": trigger.interrupt,
        })
    }

    fn subscription(subscription: &proto::Subscription) -> Value {
        json!({
            "topic": subscription.topic,
            "mode": subscription.mode,
            "from": subscription.from,
            "urgent": subscription.urgent,
            "objective": subscription.objective,
            "status": subscription.status,
            "askedBy": subscription.asked_by,
            "why": subscription.why,
            "askedAt": Wire::time(subscription.asked_at.as_ref()),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{AutonomyLevel, BotStatus, TriggerType};

    fn message() -> proto::Bot {
        proto::Bot {
            id: "bot-a".into(),
            name: "bot-a".into(),
            mission: "Do it.".into(),
            level: "observe".into(),
            status: "Enabled".into(),
            allowed_tools: vec!["local_read".into()],
            model: Some(proto::ModelPolicy {
                model: "anthropic/claude-haiku-5.5".into(),
                max_context_tokens: 32000,
                ..Default::default()
            }),
            budgets: Some(proto::Budgets {
                task_timeout: Some(Wire::parse_duration("5m").unwrap()),
                ..Default::default()
            }),
            triggers: vec![
                proto::Trigger {
                    r#type: "manual".into(),
                    ..Default::default()
                },
                proto::Trigger {
                    r#type: "timer".into(),
                    every: Some(Wire::parse_duration("10m").unwrap()),
                    ..Default::default()
                },
            ],
            placement: Some(proto::Placement {
                nodes: vec!["wrx80".into()],
                ..Default::default()
            }),
            created_at: Some(Wire::timestamp("2026-10-24T22:30:00Z".parse().unwrap())),
            updated_at: Some(Wire::timestamp("2026-10-24T22:31:00Z".parse().unwrap())),
            staged: Some(proto::StagedDefinition {
                version: "1.3.0".into(),
                staged_at: Some(Wire::timestamp("2026-11-12T21:00:00Z".parse().unwrap())),
                change: Vec::new(),
            }),
            ..Default::default()
        }
    }

    #[test]
    fn reads_a_bot() {
        let bot = BotMapper::bot(Some(message())).unwrap();
        assert_eq!((bot.level, bot.status), (AutonomyLevel::Observe, BotStatus::Enabled));
        assert_eq!(bot.model.max_context_tokens, 32000);
        assert_eq!(bot.budgets.task_timeout_seconds, 300);
        assert_eq!(bot.triggers[1].trigger_type, TriggerType::Timer);
        assert_eq!(bot.triggers[1].every, "10m");
        assert_eq!(bot.placement.describe(), "wrx80");
        assert_eq!(bot.staged.unwrap().version, "1.3.0");
        assert!(bot.delegation.is_none());
    }

    #[test]
    fn a_bot_without_its_instants_or_a_bot_at_all_is_unreadable() {
        let mut broken = message();
        broken.created_at = None;
        assert!(matches!(BotMapper::bot(Some(broken)), Err(PanelError::Unreadable(_))));
        assert!(matches!(BotMapper::bot(None), Err(PanelError::Unreadable(_))));
    }

    #[test]
    fn a_newer_status_reads_as_unknown() {
        let mut newer = message();
        newer.status = "Hibernating".into();
        newer.level = "act-with-hover".into();
        let bot = BotMapper::bot(Some(newer)).unwrap();
        assert_eq!((bot.status, bot.level), (BotStatus::Unknown, AutonomyLevel::Unknown));
    }

    #[test]
    fn reads_what_a_delete_and_a_stop_answer() {
        let deleted = BotMapper::deleted(proto::DeleteBotResponse {
            bot: Some(message()),
            cancelled: vec!["task-1".into()],
            timers_removed: 2,
            named_by: vec![proto::BotReference {
                bot_id: "bot-b".into(),
                how: "delegates".into(),
                topic: String::new(),
            }],
            ..Default::default()
        })
        .unwrap();
        assert_eq!(
            (deleted.timers_removed, deleted.named_by[0].bot_id.as_str()),
            (2, "bot-b")
        );
        let stopped = BotMapper::stopped(proto::StopBotResponse {
            bot: Some(message()),
            cancelled: vec![],
            not_cancelled: vec![proto::StopRefusal {
                task_id: "task-2".into(),
                reason: "done".into(),
            }],
        })
        .unwrap();
        assert_eq!(stopped.not_cancelled[0].reason, "done");
    }
}
