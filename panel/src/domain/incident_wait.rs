use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::ApprovalMark;

/// The approval an incident's current task waits for, or last waited
/// for: the call, its tool, the deadline, how many people must approve
/// it and who did, and, once it ended, how.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IncidentWait {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub tool: String,
    pub deadline: DateTime<Utc>,
    /// approved, denied, expired or cancelled; empty while it waits.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub outcome: String,
    /// How many different people must approve the call: absent is one;
    /// 2 when the bot's twoPersons names the incident.
    #[serde(default, skip_serializing_if = "is_zero")]
    pub approvals_needed: u8,
    /// For a call that waits for two: who approved it so far, in order.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub approved_by: Vec<ApprovalMark>,
}

fn is_zero(n: &u8) -> bool {
    *n == 0
}

impl IncidentWait {
    /// Whether nobody has decided it yet.
    pub fn is_open(&self) -> bool {
        self.outcome.is_empty()
    }

    /// Where a call that waits for more than one person stands: `1 of 2
    /// approvals · approved by tirso`; None for a call one person decides.
    pub fn progress(&self) -> Option<String> {
        let needed = usize::from(self.approvals_needed);
        if needed < 2 {
            return None;
        }
        let done = format!("{} of {needed} approvals", self.approved_by.len());
        if self.approved_by.is_empty() {
            return Some(format!("{done} · two different people decide"));
        }
        let names: Vec<&str> = self.approved_by.iter().map(|m| m.actor.as_str()).collect();
        Some(format!("{done} · approved by {}", names.join(", ")))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_how_far_a_two_person_wait_is() {
        let one: IncidentWait = serde_json::from_str(r#"{"callId":"c","deadline":"2026-11-08T10:00:00Z"}"#).unwrap();
        assert_eq!(one.progress(), None);
        assert!(!serde_json::to_string(&one).unwrap().contains("approvalsNeeded"));
        let none: IncidentWait =
            serde_json::from_str(r#"{"deadline":"2026-11-08T10:00:00Z","approvalsNeeded":2}"#).unwrap();
        assert_eq!(
            none.progress().as_deref(),
            Some("0 of 2 approvals · two different people decide")
        );
        let first: IncidentWait = serde_json::from_str(
            r#"{"deadline":"2026-11-08T10:00:00Z","approvalsNeeded":2,"approvedBy":[{"actor":"tirso","at":"2026-11-08T09:50:00Z"}]}"#,
        )
        .unwrap();
        assert_eq!(
            first.progress().as_deref(),
            Some("1 of 2 approvals · approved by tirso")
        );
        assert!(first.is_open());
    }
}
