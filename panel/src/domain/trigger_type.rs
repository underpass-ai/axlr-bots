use serde::{Deserialize, Serialize};

/// What starts a bot's task: a person, a webhook, a timer, another bot
/// that delegates to it, a teammate's event, an incident the control
/// plane opened for it, from Alertmanager or from CloudWatch through SNS,
/// another team's question, or the answer to a question it asked.
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
    Alertmanager,
    Cloudwatch,
    Question,
    #[serde(rename = "question_answer")]
    QuestionAnswer,
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
            Self::Alertmanager => "alertmanager",
            Self::Cloudwatch => "cloudwatch",
            Self::Question => "question",
            Self::QuestionAnswer => "question_answer",
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
            Self::Alertmanager => "Alertmanager",
            Self::Cloudwatch => "CloudWatch",
            Self::Question => "Another team's question",
            Self::QuestionAnswer => "The answer it asked for",
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
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"cloudwatch\"").unwrap(),
            TriggerType::Cloudwatch
        );
        assert_eq!(TriggerType::Cloudwatch.label(), "CloudWatch");
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
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"alertmanager\"").unwrap(),
            TriggerType::Alertmanager
        );
        assert_eq!(TriggerType::Alertmanager.label(), "Alertmanager");
    }

    #[test]
    fn reads_the_team_channel_s_triggers() {
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"question\"").unwrap(),
            TriggerType::Question
        );
        assert_eq!(
            serde_json::from_str::<TriggerType>("\"question_answer\"").unwrap(),
            TriggerType::QuestionAnswer
        );
        assert_eq!(TriggerType::QuestionAnswer.name(), "question_answer");
        assert_eq!(TriggerType::Question.label(), "Another team's question");
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
