use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{
    AutonomyLevel, BotApprovals, BotDelegation, BotId, BotStatus, BotUpdate, Budgets, ModelPolicy, Placement,
    StagedDefinition, Subscription, ToolRules, Trigger, Version,
};

/// A bot as the control plane reports it: a durable resource with a
/// mission, a model, tools, a level, triggers, budgets and a placement.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Bot {
    pub id: BotId,
    #[serde(default)]
    pub tenant_id: String,
    pub name: String,
    #[serde(default)]
    pub owner: String,
    pub mission: String,
    #[serde(default)]
    pub allowed_tools: Vec<String>,
    #[serde(default)]
    pub model: ModelPolicy,
    #[serde(default)]
    pub budgets: Budgets,
    #[serde(default)]
    pub level: AutonomyLevel,
    #[serde(default)]
    pub tool_rules: ToolRules,
    /// The calls it may make without a person.
    #[serde(default)]
    pub approvals: BotApprovals,
    /// The bots outside its teams it may ask for a task.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub delegation: Option<BotDelegation>,
    #[serde(default)]
    pub triggers: Vec<Trigger>,
    #[serde(default)]
    pub placement: Placement,
    /// The teams it belongs to.
    #[serde(default)]
    pub groups: Vec<String>,
    /// The topics it listens to; a bot's asks are pending.
    #[serde(default)]
    pub subscriptions: Vec<Subscription>,
    /// Its definition's version, as its definition names it; empty: none.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub version: String,
    /// The next version of its definition, staged and not applied yet.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub staged: Option<StagedDefinition>,
    pub status: BotStatus,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Bot {
    /// Its teams, `ops, lab`, or empty.
    pub fn team_summary(&self) -> String {
        self.groups.join(", ")
    }

    /// Its asks to hear a topic that wait for a person.
    pub fn pending_asks(&self) -> impl Iterator<Item = &Subscription> {
        self.subscriptions.iter().filter(|s| s.is_pending())
    }

    /// Its definition's version as the panel writes it, `v1.2.0`, or as
    /// written when it is not one the panel can read; none when unset.
    pub fn version_label(&self) -> Option<String> {
        let raw = self.version.trim();
        if raw.is_empty() {
            return None;
        }
        Some(Version::parse(raw).map_or_else(|| raw.to_string(), |v| v.label()))
    }

    /// The staged version it can update to, when that one is newer.
    pub fn update(&self) -> Option<BotUpdate> {
        BotUpdate::of(self)
    }

    /// The trigger types, `manual · timer · webhook`.
    pub fn trigger_summary(&self) -> String {
        let names: Vec<&str> = self.triggers.iter().map(|t| t.trigger_type.name()).collect();
        if names.is_empty() {
            "none".to_string()
        } else {
            names.join(" · ")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_api_json() {
        let bot: Bot = serde_json::from_str(
            r#"{"id":"bot-wrx80","tenantId":"default","name":"bot-wrx80","owner":"operator","mission":"Do it.",
            "allowedTools":["local_read"],"model":{"model":"anthropic/claude-haiku-5.5","routingPolicy":"cost-optimized","allowRemoteProviders":true,"maxContextTokens":32000},
            "budgets":{"maxModelCallsPerTask":10,"maxToolCallsPerTask":20,"maxDelegationDepth":0,"taskTimeoutSeconds":300},
            "level":"observe","toolRules":{},"triggers":[{"type":"manual"},{"type":"timer","name":"clock","every":"10m"}],
            "placement":{"nodes":["wrx80"]},"status":"Enabled","createdAt":"2026-10-24T22:30:00Z","updatedAt":"2026-10-24T22:31:00Z"}"#,
        )
        .unwrap();
        assert_eq!(bot.id.as_str(), "bot-wrx80");
        assert_eq!(bot.level, AutonomyLevel::Observe);
        assert_eq!(bot.placement.describe(), "wrx80");
        assert_eq!(bot.trigger_summary(), "manual · timer");
        assert_eq!(bot.model.short_model(), "claude-haiku-5.5");
        assert_eq!((bot.version_label(), bot.staged.as_ref()), (None, None));
        let mut none = bot.clone();
        none.triggers.clear();
        assert_eq!(none.trigger_summary(), "none");
    }

    #[test]
    fn reads_its_version_and_the_staged_one() {
        let bot: Bot = serde_json::from_str(
            r#"{"id":"bot-a","name":"bot-a","mission":"Do it.","version":"1.2.0",
            "staged":{"version":"1.3.0","stagedAt":"2026-11-12T21:00:00Z"},
            "status":"Enabled","createdAt":"2026-10-24T22:30:00Z","updatedAt":"2026-10-24T22:31:00Z"}"#,
        )
        .unwrap();
        assert_eq!(bot.version_label().as_deref(), Some("v1.2.0"));
        assert_eq!(bot.staged.as_ref().unwrap().version, "1.3.0");
        assert_eq!(bot.update().unwrap().tip(), "v1.2.0 → v1.3.0 ready");
        let mut odd = bot.clone();
        odd.version = "nightly".into();
        assert_eq!(odd.version_label().as_deref(), Some("nightly"));
        let json = serde_json::to_string(&Bot {
            version: String::new(),
            staged: None,
            ..bot
        })
        .unwrap();
        assert!(!json.contains("version") && !json.contains("staged"), "{json}");
    }
}
