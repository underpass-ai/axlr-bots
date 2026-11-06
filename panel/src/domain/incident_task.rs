use serde::{Deserialize, Serialize};

use super::{BotId, IncidentPhase, TaskId};

/// A task that worked on an incident: which, by which bot, for which
/// phase and in which cycle.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IncidentTask {
    pub task_id: TaskId,
    pub bot_id: BotId,
    #[serde(default)]
    pub phase: IncidentPhase,
    #[serde(default)]
    pub cycle: u32,
}
