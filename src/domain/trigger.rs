use serde::{Deserialize, Serialize};

use super::TriggerType;

/// What starts a bot's task: by hand, a webhook (credentials redacted by
/// the control plane) or a timer.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Trigger {
    #[serde(rename = "type")]
    pub trigger_type: TriggerType,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub objective: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub secret: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub signing_key: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub every: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub cron: String,
}

impl Trigger {
    /// The trigger by hand: what a new bot gets.
    pub fn manual() -> Self {
        Self::default()
    }

    /// The name, which defaults to the type.
    pub fn display_name(&self) -> &str {
        if self.name.is_empty() {
            self.trigger_type.name()
        } else {
            &self.name
        }
    }

    /// When a timer fires: `every 10m` or `cron 0 3 * * *`; empty otherwise.
    pub fn schedule(&self) -> String {
        if !self.every.is_empty() {
            format!("every {}", self.every)
        } else if !self.cron.is_empty() {
            format!("cron {}", self.cron)
        } else {
            String::new()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_and_schedules() {
        let timer: Trigger =
            serde_json::from_str(r#"{"type":"timer","name":"clock","every":"10m"}"#).unwrap();
        assert_eq!(timer.display_name(), "clock");
        assert_eq!(timer.schedule(), "every 10m");
        let nightly: Trigger =
            serde_json::from_str(r#"{"type":"timer","cron":"0 3 * * *"}"#).unwrap();
        assert_eq!(nightly.display_name(), "timer");
        assert_eq!(nightly.schedule(), "cron 0 3 * * *");
        assert_eq!(Trigger::manual().schedule(), "");
        assert_eq!(
            serde_json::to_string(&Trigger::manual()).unwrap(),
            r#"{"type":"manual"}"#
        );
    }
}
