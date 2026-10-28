use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{AutonomyLevel, BotId, BotStatus, Budgets, ModelPolicy, Placement, Subscription, ToolRules, Trigger};

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
    pub status: BotStatus,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Bot {
    /// Whether it may fly or drive home: its tools include `return_home`.
    pub fn can_return_home(&self) -> bool {
        self.allowed_tools.iter().any(|t| t == super::RETURN_HOME_TOOL)
    }

    /// Its teams, `port, sea`, or empty.
    pub fn team_summary(&self) -> String {
        self.groups.join(", ")
    }

    /// Its asks to hear a topic that wait for a person.
    pub fn pending_asks(&self) -> impl Iterator<Item = &Subscription> {
        self.subscriptions.iter().filter(|s| s.is_pending())
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
        let mut none = bot.clone();
        none.triggers.clear();
        assert_eq!(none.trigger_summary(), "none");
    }
}
