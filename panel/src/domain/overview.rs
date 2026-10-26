use std::collections::BTreeMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{BotStatus, NodeStatus, TaskStatus};

/// The dashboard's counts: what the control plane holds now and what
/// happened since a moment.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Overview {
    pub since: Option<DateTime<Utc>>,
    #[serde(default)]
    pub nodes: BTreeMap<String, u32>,
    #[serde(default)]
    pub bots: BTreeMap<String, u32>,
    #[serde(default)]
    pub tasks: BTreeMap<String, u32>,
    #[serde(default)]
    pub tasks_since: BTreeMap<String, u32>,
    #[serde(default)]
    pub executions_since: u32,
    #[serde(default, rename = "costUsdSince")]
    pub cost_usd_since: f64,
}

impl Overview {
    pub fn nodes_in(&self, status: NodeStatus) -> u32 {
        self.nodes.get(status.name()).copied().unwrap_or(0)
    }

    pub fn bots_in(&self, status: BotStatus) -> u32 {
        self.bots.get(status.name()).copied().unwrap_or(0)
    }

    /// Tasks in the status now.
    pub fn tasks_in(&self, status: TaskStatus) -> u32 {
        self.tasks.get(status.name()).copied().unwrap_or(0)
    }

    /// Tasks created since the moment that are in the status now.
    pub fn tasks_since_in(&self, status: TaskStatus) -> u32 {
        self.tasks_since.get(status.name()).copied().unwrap_or(0)
    }

    pub fn total_nodes(&self) -> u32 {
        self.nodes.values().sum()
    }

    pub fn total_bots(&self) -> u32 {
        self.bots.values().sum()
    }

    /// Nodes not Online and not Revoked: the ones that need a look.
    pub fn nodes_down(&self) -> u32 {
        self.nodes_in(NodeStatus::Offline) + self.nodes_in(NodeStatus::Registered)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_counts() {
        let overview: Overview = serde_json::from_str(
            r#"{"since":"2026-10-24T22:00:00Z","nodes":{"Offline":1,"Online":2,"Registered":0,"Revoked":1},
            "bots":{"Enabled":3,"Paused":1},"tasks":{"Running":1,"WaitingApproval":2,"Completed":40},
            "tasksSince":{"Completed":4,"Failed":1,"Expired":5},"executionsSince":6,"costUsdSince":0.0123}"#,
        )
        .unwrap();
        assert_eq!(overview.nodes_in(NodeStatus::Online), 2);
        assert_eq!(overview.total_nodes(), 4);
        assert_eq!(overview.nodes_down(), 1);
        assert_eq!(overview.bots_in(BotStatus::Paused), 1);
        assert_eq!(overview.bots_in(BotStatus::Draft), 0);
        assert_eq!(overview.total_bots(), 4);
        assert_eq!(overview.tasks_in(TaskStatus::WaitingApproval), 2);
        assert_eq!(overview.tasks_since_in(TaskStatus::Failed), 1);
        assert_eq!(overview.tasks_since_in(TaskStatus::Expired), 5);
        assert_eq!(overview.tasks_since_in(TaskStatus::Pending), 0);
        assert_eq!(overview.executions_since, 6);
        assert!((overview.cost_usd_since - 0.0123).abs() < 1e-9);
    }
}
