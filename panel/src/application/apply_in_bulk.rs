use std::sync::Arc;

use futures::StreamExt;

use super::{BulkAction, BulkOutcome, BulkTarget, CancelTask, ControlPlane, DecideTask, DeleteBot, PanelError};
use super::{RevokeNode, StopBot, SwitchBot};
use crate::domain::Decision;

/// How many calls of one bulk act are on their way at once: enough to be
/// quick, few enough not to flood the control plane.
pub const AT_ONCE: usize = 4;

/// Does one act to many rows: one call per row, the call its row's
/// button makes, a few at a time, and one outcome for all.
pub struct ApplyInBulk {
    control_plane: Arc<dyn ControlPlane>,
}

impl ApplyInBulk {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, action: BulkAction, targets: Vec<BulkTarget>) -> BulkOutcome {
        let answers: Vec<(String, Result<(), PanelError>)> = futures::stream::iter(targets)
            .map(|target| async move { (target.name(), self.one(action, target).await) })
            .buffered(AT_ONCE)
            .collect()
            .await;
        let mut outcome = BulkOutcome::new(action);
        for (name, result) in answers {
            outcome.record(name, result);
        }
        outcome
    }

    async fn one(&self, action: BulkAction, target: BulkTarget) -> Result<(), PanelError> {
        let cp = self.control_plane.clone();
        match (action, target) {
            (BulkAction::TurnOn, BulkTarget::Bot(id)) => SwitchBot::new(cp).enable(id).await.map(drop),
            (BulkAction::Pause, BulkTarget::Bot(id)) => SwitchBot::new(cp).pause(id).await.map(drop),
            (BulkAction::Stop, BulkTarget::Bot(id)) => StopBot::new(cp).execute(id).await.map(drop),
            (BulkAction::Delete, BulkTarget::Bot(id)) => DeleteBot::new(cp).delete(id).await.map(drop),
            (BulkAction::Approve, BulkTarget::Task { id, call, .. }) => {
                DecideTask::new(cp).execute(id, Decision::Approve, call).await.map(drop)
            }
            (BulkAction::Deny, BulkTarget::Task { id, call, .. }) => {
                DecideTask::new(cp).execute(id, Decision::Deny, call).await.map(drop)
            }
            (BulkAction::Cancel, BulkTarget::Task { id, .. }) => CancelTask::new(cp).execute(id).await.map(drop),
            (BulkAction::Revoke, BulkTarget::Node(id)) => RevokeNode::new(cp).execute(id).await.map(drop),
            (action, target) => Err(PanelError::Invalid(format!(
                "{} does not apply to {}",
                action.label(),
                target.name()
            ))),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{BotId, NodeId, TaskId};

    fn bot(id: &str) -> BulkTarget {
        BulkTarget::Bot(BotId::new(id).unwrap())
    }

    #[test]
    fn makes_one_call_per_row_and_counts_the_answers() {
        let (cp, _) = fleet();
        let outcome = run(ApplyInBulk::new(cp.clone()).execute(
            BulkAction::Pause,
            vec![bot("bot"), bot("bot-wrx80"), bot("no-such-bot")],
        ));
        assert_eq!(outcome.done, vec!["bot", "bot-wrx80"]);
        assert_eq!(outcome.failed.len(), 1);
        assert_eq!(outcome.failed[0].name, "no-such-bot");
        assert_eq!(outcome.title(), "2 paused · 1 failed");
        let turned = run(ApplyInBulk::new(cp.clone()).execute(BulkAction::TurnOn, vec![bot("bot")]));
        assert_eq!(turned.title(), "1 turned on");
        let stopped = run(ApplyInBulk::new(cp.clone()).execute(BulkAction::Stop, vec![bot("bot")]));
        assert_eq!(stopped.title(), "1 stopped");
        let deleted = run(ApplyInBulk::new(cp.clone()).execute(BulkAction::Delete, vec![bot("bot-wrx80")]));
        assert_eq!(deleted.title(), "1 deleted");
        assert!(
            run(cp.list_bots())
                .unwrap()
                .iter()
                .all(|b| b.id.as_str() != "bot-wrx80")
        );
    }

    #[test]
    fn decides_stops_and_revokes() {
        let (cp, _) = fleet();
        let tasks = run(cp.list_tasks(crate::domain::TaskFilter::with_limit(50))).unwrap();
        let waiting = tasks
            .iter()
            .find(|t| t.status == crate::domain::TaskStatus::WaitingApproval)
            .unwrap();
        let running = tasks
            .iter()
            .find(|t| t.status == crate::domain::TaskStatus::Running)
            .unwrap();
        let uc = ApplyInBulk::new(cp.clone());
        let approved = run(uc.execute(
            BulkAction::Approve,
            vec![BulkTarget::Task {
                id: waiting.id.clone(),
                call: Some("call-1".into()),
                label: String::new(),
            }],
        ));
        assert_eq!(approved.title(), "1 approved");
        let cancelled = run(uc.execute(
            BulkAction::Cancel,
            vec![BulkTarget::Task {
                id: running.id.clone(),
                call: None,
                label: String::new(),
            }],
        ));
        assert_eq!(cancelled.title(), "1 stopped");
        let denied = run(uc.execute(
            BulkAction::Deny,
            vec![BulkTarget::Task {
                id: TaskId::new("task-none").unwrap(),
                call: None,
                label: String::new(),
            }],
        ));
        assert!(denied.all_failed());
        let revoked = run(uc.execute(
            BulkAction::Revoke,
            vec![BulkTarget::Node(NodeId::new("wrx80").unwrap())],
        ));
        assert_eq!(revoked.title(), "1 revoked");
    }

    #[test]
    fn refuses_an_act_on_a_row_it_does_not_fit() {
        let (cp, _) = fleet();
        let outcome = run(ApplyInBulk::new(cp).execute(BulkAction::Revoke, vec![bot("bot")]));
        assert!(outcome.all_failed());
        assert_eq!(outcome.detail(), "bot: Revoke does not apply to bot");
    }
}
