use serde::{Deserialize, Serialize};

/// What starts a bot's task: a person, a webhook, a timer, another bot
/// that delegates to it, a teammate's event, or an incident the control
/// plane opened for it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TriggerType {
    #[default]
    Manual,
    Webhook,
    Timer,
    Delegation,
    Event,
    Incident,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    #[serde(other)]
    Unknown,
}

impl TriggerType {
    pub fn name(self) -> &'static str {
        match self {
            Self::Manual => "manual",
            Self::Webhook => "webhook",
            Self::Timer => "timer",
            Self::Delegation => "delegation",
            Self::Event => "event",
            Self::Incident => "incident",
            Self::Unknown => "unknown",
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Self::Manual => "By hand",
            Self::Webhook => "Webhook",
            Self::Timer => "Timer",
            Self::Delegation => "Another bot",
            Self::Event => "A teammate's event",
            Self::Incident => "An incident",
            Self::Unknown => "Unknown",
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
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"cron\"").unwrap(),
            TriggerType::Unknown
        );
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

    #[test]
    fn reads_an_incident_s_trigger() {
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"incident\"").unwrap(),
            TriggerType::Incident
        );
        assert_eq!(TriggerType::Incident.name(), "incident");
        assert_eq!(TriggerType::Incident.label(), "An incident");
    }

    #[test]
    fn reads_a_teammate_s_event() {
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"event\"").unwrap(),
            TriggerType::Event
        );
        assert_eq!(TriggerType::Event.name(), "event");
        assert_eq!(TriggerType::Event.label(), "A teammate's event");
    }
}
