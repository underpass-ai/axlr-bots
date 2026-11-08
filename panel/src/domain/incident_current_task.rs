use serde::{Deserialize, Serialize};

use super::{BotId, IncidentPhase, IncidentWait, TaskId, TaskStatus};

/// The task an incident's next move waits on, as `GET /v1/incidents/{id}`
/// says it: its bot, phase and status, and the approval it waits for.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IncidentCurrentTask {
    pub task_id: TaskId,
    pub bot_id: BotId,
    #[serde(default)]
    pub phase: IncidentPhase,
    pub status: TaskStatus,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub approval: Option<IncidentWait>,
}

impl IncidentCurrentTask {
    /// The call a person decides now, while the task waits on one.
    pub fn waiting_call(&self) -> Option<&IncidentWait> {
        self.approval
            .as_ref()
            .filter(|w| w.is_open() && self.status == TaskStatus::WaitingApproval)
    }
}
