use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::AutonomyCounters;

/// One line of a runbook's history: a signal, the cycle it came from and
/// any step it moved.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AutonomyEntry {
    pub signal: String,
    pub incident_id: String,
    pub cycle: u32,
    #[serde(default)]
    pub actor: String,
    pub at: DateTime<Utc>,
    #[serde(default)]
    pub from: String,
    #[serde(default)]
    pub to: String,
    #[serde(default)]
    pub earned_before: Option<AutonomyCounters>,
}

impl AutonomyEntry {
    /// The step it moved, in words, when it moved one.
    pub fn moved(&self) -> Option<String> {
        (!self.from.is_empty() && !self.to.is_empty()).then(|| format!("{} → {}", self.from, self.to))
    }
}
