use serde::{Deserialize, Serialize};

/// What starts a bot's task: a person, a webhook, a timer or another bot
/// that delegates to it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TriggerType {
    #[default]
    Manual,
    Webhook,
    Timer,
    Delegation,
}

impl TriggerType {
    pub fn name(self) -> &'static str {
        match self {
            Self::Manual => "manual",
            Self::Webhook => "webhook",
            Self::Timer => "timer",
            Self::Delegation => "delegation",
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Self::Manual => "By hand",
            Self::Webhook => "Webhook",
            Self::Timer => "Timer",
            Self::Delegation => "Another bot",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_lowercase_words() {
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"timer\"").unwrap(),
            TriggerType::Timer
        );
        assert!(serde_json::from_str::<TriggerType>("\"cron\"").is_err());
        assert_eq!(TriggerType::Manual.label(), "By hand");
        assert_eq!(TriggerType::Webhook.name(), "webhook");
    }

    #[test]
    fn reads_a_delegated_task_s_trigger() {
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"delegation\"").unwrap(),
            TriggerType::Delegation
        );
        assert_eq!(TriggerType::Delegation.name(), "delegation");
        assert_eq!(TriggerType::Delegation.label(), "Another bot");
    }
}
