use chrono::DateTime;
use serde_json::Value;

use super::json_fields::JsonFields;
use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{BotDefinition, Budgets, ModelPolicy, Placement, Subscription, ToolRule, ToolRules, Trigger};

/// A bot definition the operator registers, into the wire message. The
/// parts the panel keeps as JSON (approvals, delegation, events, runtime,
/// skills) are read field by field: a field the contract does not name is
/// refused, never dropped on the way.
pub(super) struct DefinitionMapper;

impl DefinitionMapper {
    pub fn definition(definition: &BotDefinition) -> Result<proto::BotDefinition, PanelError> {
        Ok(proto::BotDefinition {
            id: definition.id.clone(),
            version: definition.version.clone(),
            tenant: definition.tenant.clone(),
            name: definition.name.clone(),
            owner: definition.owner.clone(),
            mission: definition.mission.clone(),
            allowed_tools: definition.allowed_tools.clone(),
            level: definition.level.name().to_string(),
            tool_rules: Some(Self::tool_rules(&definition.tool_rules)),
            triggers: Self::triggers(&definition.triggers)?,
            placement: Some(Self::placement(&definition.placement)),
            approvals: definition.approvals.as_ref().map(Self::approvals).transpose()?,
            delegation: definition.delegation.as_ref().map(Self::delegation).transpose()?,
            groups: definition.groups.clone(),
            events: definition.events.as_ref().map(Self::events).transpose()?,
            subscriptions: Vec::new(),
            runtime: definition.runtime.as_ref().map(Self::runtime).transpose()?,
            skills: Self::skills(definition.skills.as_ref())?,
            model: Some(Self::model(&definition.model)),
            budgets: Some(Self::budgets(&definition.budgets)),
        })
    }

    pub fn model(model: &ModelPolicy) -> proto::ModelPolicy {
        proto::ModelPolicy {
            model: model.model.clone(),
            routing_policy: model.routing_policy.clone(),
            allow_remote_providers: model.allow_remote_providers,
            max_context_tokens: i32::try_from(model.max_context_tokens).unwrap_or(i32::MAX),
        }
    }

    pub fn budgets(budgets: &Budgets) -> proto::Budgets {
        let int = |n: u32| i32::try_from(n).unwrap_or(i32::MAX);
        proto::Budgets {
            max_model_calls_per_task: int(budgets.max_model_calls_per_task),
            max_tool_calls_per_task: int(budgets.max_tool_calls_per_task),
            max_delegation_depth: int(budgets.max_delegation_depth),
            task_timeout: (budgets.task_timeout_seconds > 0).then(|| prost_types::Duration {
                seconds: i64::from(budgets.task_timeout_seconds),
                nanos: 0,
            }),
        }
    }

    pub fn rule(rule: &ToolRule) -> proto::ToolRule {
        proto::ToolRule {
            tool: rule.tool.clone(),
            arguments: rule.arguments.clone().into_iter().collect(),
        }
    }

    pub fn rules(rules: &[ToolRule]) -> Vec<proto::ToolRule> {
        rules.iter().map(Self::rule).collect()
    }

    pub fn tool_rules(rules: &ToolRules) -> proto::ToolRules {
        proto::ToolRules {
            allow: Self::rules(&rules.allow),
            deny: Self::rules(&rules.deny),
        }
    }

    pub fn placement(placement: &Placement) -> proto::Placement {
        proto::Placement {
            nodes: placement.nodes.iter().map(ToString::to_string).collect(),
            selector: placement.selector.clone().into_iter().collect(),
        }
    }

    pub fn triggers(triggers: &[Trigger]) -> Result<Vec<proto::Trigger>, PanelError> {
        triggers
            .iter()
            .map(|t| {
                Ok(proto::Trigger {
                    r#type: t.trigger_type.name().to_string(),
                    name: t.name.clone(),
                    objective: t.objective.clone(),
                    secret: t.secret.clone(),
                    signing_key: t.signing_key.clone(),
                    every: (!t.every.is_empty())
                        .then(|| Wire::parse_duration(&t.every))
                        .transpose()?,
                    cron: t.cron.clone(),
                    events: t.events.clone(),
                    interrupt: t.interrupt,
                })
            })
            .collect()
    }

    pub fn subscriptions(subscriptions: &[Subscription]) -> Result<Vec<proto::Subscription>, PanelError> {
        subscriptions
            .iter()
            .map(|s| {
                let asked_at = match &s.asked_at {
                    None => None,
                    Some(text) => Some(Wire::timestamp(
                        DateTime::parse_from_rfc3339(text)
                            .map_err(|e| PanelError::Invalid(format!("askedAt {text:?}: {e}")))?
                            .to_utc(),
                    )),
                };
                Ok(proto::Subscription {
                    topic: s.topic.clone(),
                    mode: s.mode.clone(),
                    from: s.from.clone(),
                    urgent: s.urgent,
                    objective: s.objective.clone(),
                    status: s.status.clone(),
                    asked_by: s.asked_by.clone(),
                    why: s.why.clone(),
                    asked_at,
                })
            })
            .collect()
    }

    fn rule_objects(fields: Vec<JsonFields>) -> Result<Vec<proto::ToolRule>, PanelError> {
        fields
            .into_iter()
            .map(|mut f| {
                let rule = proto::ToolRule {
                    tool: f.string("tool")?,
                    arguments: f.string_map("arguments")?,
                };
                f.finish()?;
                Ok(rule)
            })
            .collect()
    }

    fn approvals(value: &Value) -> Result<proto::Approvals, PanelError> {
        let mut f = JsonFields::of("approvals", value)?;
        let ttl = f.string("ttl")?;
        let notify = f
            .objects("approvals.notify", "notify")?
            .into_iter()
            .map(|mut n| {
                let target = proto::NotifyTarget {
                    r#type: n.string("type")?,
                    name: n.string("name")?,
                    url: n.string("url")?,
                    secret: n.string("secret")?,
                    command: n.strings("command")?,
                };
                n.finish()?;
                Ok(target)
            })
            .collect::<Result<_, PanelError>>()?;
        let approvals = proto::Approvals {
            ttl: (!ttl.is_empty()).then(|| Wire::parse_duration(&ttl)).transpose()?,
            on_expire: f.string("onExpire")?,
            notify,
            auto_approve: Self::rule_objects(f.objects("approvals.autoApprove", "autoApprove")?)?,
            approvers: f.strings("approvers")?,
        };
        f.finish()?;
        Ok(approvals)
    }

    fn delegation(value: &Value) -> Result<proto::Delegation, PanelError> {
        let mut f = JsonFields::of("delegation", value)?;
        let delegation = proto::Delegation {
            to: f.strings("to")?,
            max_depth: f.int("maxDepth")?,
            budget_share: f.number("budgetShare")?,
            split: f.string("split")?,
            max_children: f.int("maxChildren")?,
            from_team: f.optional_flag("fromTeam")?,
        };
        f.finish()?;
        Ok(delegation)
    }

    fn events(value: &Value) -> Result<proto::Events, PanelError> {
        let mut f = JsonFields::of("events", value)?;
        let emit = f
            .objects("events.emit", "emit")?
            .into_iter()
            .map(|mut e| {
                let rule = proto::EmitRule {
                    name: e.string("name")?,
                    max_severity: e.string("maxSeverity")?,
                };
                e.finish()?;
                Ok(rule)
            })
            .collect::<Result<_, PanelError>>()?;
        let events = proto::Events {
            emit,
            per_minute: f.int("perMinute")?,
            intake_per_minute: f.int("intakePerMinute")?,
            max_depth: f.int("maxDepth")?,
            interruptible: f.optional_flag("interruptible")?,
        };
        f.finish()?;
        Ok(events)
    }

    fn runtime(value: &Value) -> Result<proto::Runtime, PanelError> {
        let mut f = JsonFields::of("runtime", value)?;
        let kind = f.string("kind")?;
        let jev = f.object("runtime.jev", "jev")?.map(Self::jev).transpose()?;
        f.finish()?;
        Ok(proto::Runtime { kind, jev })
    }

    fn jev(mut f: JsonFields) -> Result<proto::JevProgram, PanelError> {
        let act = match f.object("runtime.jev.act", "act")? {
            None => None,
            Some(mut a) => {
                let act = proto::JevAct {
                    tool: a.string("tool")?,
                    choice: a.string("choice")?,
                    verdict: a.string("verdict")?,
                    pass: a.strings("pass")?,
                };
                a.finish()?;
                Some(act)
            }
        };
        let tell = match f.object("runtime.jev.tell", "tell")? {
            None => None,
            Some(mut t) => {
                let tell = proto::JevTell {
                    name: t.string("name")?,
                    fields: t.strings("fields")?,
                };
                t.finish()?;
                Some(tell)
            }
        };
        let program = proto::JevProgram {
            server: f.string("server")?,
            start: f.string("start")?,
            act,
            wait: f.string("wait")?,
            stop: f.string("stop")?,
            tell,
            model: f.string("model")?,
        };
        f.finish()?;
        Ok(program)
    }

    fn skills(value: Option<&Value>) -> Result<Vec<proto::SkillPin>, PanelError> {
        let Some(value) = value else { return Ok(Vec::new()) };
        let Value::Array(items) = value else {
            return Err(PanelError::Invalid("skills must be a list".into()));
        };
        items
            .iter()
            .map(|item| {
                let mut f = JsonFields::of("skills", item)?;
                let pin = proto::SkillPin {
                    name: f.string("name")?,
                    version: f.string("version")?,
                    catalog: f.string("catalog")?,
                };
                f.finish()?;
                Ok(pin)
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn definition() -> BotDefinition {
        BotDefinition::from_json(
            &json!({
                "id": "bot-a", "tenant": "default", "name": "Bot A", "owner": "operator", "mission": "Do it.",
                "allowedTools": ["local_read"], "level": "act-with-approval",
                "toolRules": {"allow": [{"tool": "local_write", "arguments": {"path": "a/*"}}]},
                "triggers": [{"type": "manual"}, {"type": "timer", "name": "clock", "every": "1h30m"}],
                "placement": {"nodes": ["wrx80"], "selector": {"zone": "lab"}},
                "model": {"model": "anthropic/claude-haiku-5.5", "allowRemoteProviders": true},
                "budgets": {"maxModelCallsPerTask": 10, "taskTimeoutSeconds": 300},
                "approvals": {"ttl": "10m", "onExpire": "deny", "approvers": ["alice"],
                              "notify": [{"type": "webhook", "url": "https://x"}],
                              "autoApprove": [{"tool": "local_read"}]},
                "delegation": {"to": ["bot-b"], "maxDepth": 1, "budgetShare": 0.5, "fromTeam": false},
                "events": {"emit": [{"name": "info.x", "maxSeverity": "warning"}], "perMinute": 3},
                "runtime": {"kind": "jev", "jev": {"server": "s", "start": "go", "act": {"tool": "t", "choice": "c"}, "wait": "w"}},
                "skills": [{"name": "review", "version": "1.0.0", "catalog": "main"}],
                "groups": ["team-a"], "version": "1.2.0"
            })
            .to_string(),
        )
        .unwrap()
    }

    #[test]
    fn carries_every_part_of_a_definition() {
        let message = DefinitionMapper::definition(&definition()).unwrap();
        assert_eq!(
            (message.level.as_str(), message.version.as_str()),
            ("act-with-approval", "1.2.0")
        );
        assert_eq!(message.tool_rules.unwrap().allow[0].arguments["path"], "a/*");
        assert_eq!(message.triggers[1].every.unwrap().seconds, 5400);
        assert_eq!(message.placement.unwrap().selector["zone"], "lab");
        assert_eq!(message.budgets.unwrap().task_timeout.unwrap().seconds, 300);
        let approvals = message.approvals.unwrap();
        assert_eq!(
            (approvals.ttl.unwrap().seconds, approvals.approvers),
            (600, vec!["alice".to_string()])
        );
        assert_eq!(approvals.auto_approve[0].tool, "local_read");
        assert_eq!(approvals.notify[0].url, "https://x");
        assert_eq!(message.delegation.unwrap().from_team, Some(false));
        assert_eq!(message.events.unwrap().emit[0].max_severity, "warning");
        assert_eq!(message.runtime.unwrap().jev.unwrap().act.unwrap().choice, "c");
        assert_eq!(message.skills[0].catalog, "main");
    }

    #[test]
    fn a_field_the_contract_does_not_name_is_refused() {
        let mut with_extra = definition();
        with_extra.delegation = Some(json!({"to": ["bot-b"], "everyone": true}));
        let error = DefinitionMapper::definition(&with_extra).unwrap_err();
        assert!(
            matches!(&error, PanelError::Invalid(m) if m.contains("delegation.everyone")),
            "{error:?}"
        );
        let mut wrong_type = definition();
        wrong_type.approvals = Some(json!({"approvers": "alice"}));
        assert!(matches!(
            DefinitionMapper::definition(&wrong_type),
            Err(PanelError::Invalid(_))
        ));
        let mut bad_skills = definition();
        bad_skills.skills = Some(json!({"name": "x"}));
        assert!(DefinitionMapper::definition(&bad_skills).is_err());
    }

    #[test]
    fn a_subscription_keeps_when_it_was_asked() {
        let subscription = Subscription {
            topic: "info.x".into(),
            asked_at: Some("2026-11-01T10:00:00Z".into()),
            ..Default::default()
        };
        let messages = DefinitionMapper::subscriptions(std::slice::from_ref(&subscription)).unwrap();
        assert_eq!(messages[0].asked_at.as_ref().unwrap().seconds, 1_793_527_200);
        let bad = Subscription {
            asked_at: Some("yesterday".into()),
            ..subscription
        };
        assert!(DefinitionMapper::subscriptions(&[bad]).is_err());
    }
}
