use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{AutonomyCounters, AutonomyEntry};

/// A runbook's standing on its bot's ladder: its step, its counters and
/// the history behind them. Earned counts since the last lowering, what
/// a promotion reads.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Autonomy {
    pub id: String,
    pub runbook: String,
    pub namespace: String,
    pub step: String,
    #[serde(default)]
    pub start: String,
    #[serde(default)]
    pub totals: AutonomyCounters,
    #[serde(default)]
    pub earned: AutonomyCounters,
    #[serde(default)]
    pub history: Vec<AutonomyEntry>,
    #[serde(default)]
    pub thresholds: Option<AutonomyCounters>,
    /// The write it was earned with.
    #[serde(default)]
    pub tool: String,
    /// The applies whose end is not recorded: while one is, the card stays.
    #[serde(default)]
    pub outstanding: Vec<serde_json::Value>,
    /// The verdict on the incident's diagnosis this cycle, when marked:
    /// `diagnosis-correct` or `diagnosis-wrong`.
    #[serde(default)]
    pub diagnosis_mark: String,
    #[serde(default)]
    pub saved: bool,
    #[serde(default)]
    pub created_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub updated_at: Option<DateTime<Utc>>,
}

impl Autonomy {
    /// Whether a person may still mark this cycle's diagnosis.
    pub fn may_mark(&self) -> bool {
        self.diagnosis_mark.is_empty()
    }

    /// `3/5 diagnoses · 2/10 proposals · 0/20 applies`: what it earned
    /// since its last lowering, against what the step takes.
    pub fn progress(&self) -> String {
        let t = self.thresholds.unwrap_or_default();
        format!(
            "{}/{} diagnoses · {}/{} proposals · {}/{} applies",
            self.earned.diagnoses, t.diagnoses, self.earned.proposals, t.proposals, self.earned.applies, t.applies
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_its_progress_and_whether_it_may_be_marked() {
        let autonomy: Autonomy = serde_json::from_str(
            r#"{"id":"ladder-1","runbook":"Drift","namespace":"shop","step":"act-with-approval","earned":{"diagnoses":3,"proposals":2,"applies":0},"thresholds":{"diagnoses":5,"proposals":10,"applies":20}}"#,
        )
        .unwrap();
        assert_eq!(autonomy.progress(), "3/5 diagnoses · 2/10 proposals · 0/20 applies");
        assert!(autonomy.may_mark());
        let marked = Autonomy {
            diagnosis_mark: "diagnosis-wrong".into(),
            ..autonomy
        };
        assert!(!marked.may_mark());
    }

    #[test]
    fn says_the_step_an_entry_moved() {
        let mut entry: AutonomyEntry =
            serde_json::from_str(r#"{"signal":"apply-denied","incidentId":"inc-1","cycle":1,"at":"2026-11-08T12:00:00Z","from":"act-with-approval","to":"propose"}"#)
                .unwrap();
        assert_eq!(entry.moved().as_deref(), Some("act-with-approval → propose"));
        entry.to = String::new();
        assert_eq!(entry.moved(), None);
    }
}
