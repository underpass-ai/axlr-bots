use serde::{Deserialize, Serialize};

use super::TriggerType;

/// What starts a bot's task: by hand, a webhook (credentials redacted by
/// the control plane), a timer, another bot, or the events of a
/// teammate it hears (`events`, names or patterns), or an incident's
/// phase (`phase`, with the `level` and `tools` it narrows the bot's to).
/// Kept as the control plane says them, so a definition read from a file
/// is sent back whole.
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
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub events: Vec<String>,
    /// An event trigger whose warnings and criticals make urgent tasks.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub interrupt: bool,
    /// An incident trigger: the phase the bot takes (triage when empty).
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub phase: String,
    /// An incident trigger: the level its phase runs at, at most the
    /// bot's; empty keeps the bot's.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub level: String,
    /// An incident trigger: the tools its phase runs with, some of the
    /// bot's; empty keeps the bot's.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub tools: Vec<String>,
    /// An apply trigger's ladder, kept as the definition says it: an edit
    /// of the bot never drops the opt-in.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ladder: Option<serde_json::Value>,
    /// The postmortem phase: the severities it writes one for; empty is
    /// sev1 and sev2.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub severities: Vec<String>,
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
    fn keeps_a_ladder_through_a_save() {
        let raw = r#"{"type":"incident","name":"apply","phase":"apply","level":"act-with-approval","tools":["patch"],"ladder":{"runbooks":[{"name":"Drift","tool":"patch","namespaces":["shop"]}]}}"#;
        let trigger: Trigger = serde_json::from_str(raw).unwrap();
        assert!(trigger.ladder.is_some());
        let again = serde_json::to_value(&trigger).unwrap();
        assert_eq!(again["ladder"]["runbooks"][0]["tool"], "patch");
    }

    #[test]
    fn keeps_the_events_an_event_trigger_hears() {
        let trigger: Trigger =
            serde_json::from_str(r#"{"type":"event","name":"deploys","events":["deploy.*"]}"#).unwrap();
        assert_eq!(trigger.trigger_type, TriggerType::Event);
        assert_eq!(trigger.events, vec!["deploy.*".to_string()]);
        assert!(
            serde_json::to_string(&trigger)
                .unwrap()
                .contains(r#""events":["deploy.*"]"#)
        );
        assert!(!serde_json::to_string(&Trigger::manual()).unwrap().contains("events"));
        let loud: Trigger =
            serde_json::from_str(r#"{"type":"event","events":["stop.halt"],"interrupt":true}"#).unwrap();
        assert!(loud.interrupt);
        assert!(!serde_json::to_string(&Trigger::manual()).unwrap().contains("interrupt"));
    }

    #[test]
    fn keeps_an_incident_phase_and_the_policy_it_narrows_to() {
        let raw = r#"{"type":"incident","name":"fix","phase":"fix","level":"propose","tools":["diff","get"]}"#;
        let trigger: Trigger = serde_json::from_str(raw).unwrap();
        assert_eq!((trigger.phase.as_str(), trigger.level.as_str()), ("fix", "propose"));
        assert_eq!(trigger.tools, vec!["diff".to_string(), "get".to_string()]);
        assert_eq!(serde_json::to_string(&trigger).unwrap(), raw);
    }

    #[test]
    fn keeps_the_severities_of_the_postmortem_phase() {
        let raw = r#"{"type":"incident","name":"scribe","phase":"postmortem","severities":["sev1","sev3"]}"#;
        let trigger: Trigger = serde_json::from_str(raw).unwrap();
        assert_eq!(trigger.severities, vec!["sev1".to_string(), "sev3".to_string()]);
        assert_eq!(serde_json::to_string(&trigger).unwrap(), raw);
    }

    #[test]
    fn names_and_schedules() {
        let timer: Trigger = serde_json::from_str(r#"{"type":"timer","name":"clock","every":"10m"}"#).unwrap();
        assert_eq!(timer.display_name(), "clock");
        assert_eq!(timer.schedule(), "every 10m");
        let nightly: Trigger = serde_json::from_str(r#"{"type":"timer","cron":"0 3 * * *"}"#).unwrap();
        assert_eq!(nightly.display_name(), "timer");
        assert_eq!(nightly.schedule(), "cron 0 3 * * *");
        assert_eq!(Trigger::manual().schedule(), "");
        assert_eq!(
            serde_json::to_string(&Trigger::manual()).unwrap(),
            r#"{"type":"manual"}"#
        );
    }
}
