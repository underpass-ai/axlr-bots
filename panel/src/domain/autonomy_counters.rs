use serde::{Deserialize, Serialize};

/// The three counts a runbook climbs by.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AutonomyCounters {
    #[serde(default)]
    pub diagnoses: u32,
    #[serde(default)]
    pub proposals: u32,
    #[serde(default)]
    pub applies: u32,
}
