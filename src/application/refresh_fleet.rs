use std::sync::Arc;

use super::{Clock, ControlPlane, Fleet, PanelError};
use crate::domain::TaskFilter;

/// Reads the fleet in one go: the overview since the start of the
/// operator's day, the nodes, the bots and the newest tasks.
pub struct RefreshFleet {
    control_plane: Arc<dyn ControlPlane>,
    clock: Arc<dyn Clock>,
}

/// How many of the newest tasks a refresh reads.
pub const RECENT_TASKS: u32 = 200;

impl RefreshFleet {
    pub fn new(control_plane: Arc<dyn ControlPlane>, clock: Arc<dyn Clock>) -> Self {
        Self { control_plane, clock }
    }

    pub async fn execute(&self) -> Result<Fleet, PanelError> {
        let since = self.clock.start_of_today();
        let (overview, nodes, bots, tasks) = futures::try_join!(
            self.control_plane.overview(since),
            self.control_plane.list_nodes(),
            self.control_plane.list_bots(),
            self.control_plane.list_tasks(TaskFilter::with_limit(RECENT_TASKS)),
        )?;
        let mut nodes = nodes;
        nodes.sort_by(|a, b| a.id.cmp(&b.id));
        let mut bots = bots;
        bots.sort_by(|a, b| a.id.cmp(&b.id));
        Ok(Fleet {
            overview,
            nodes,
            bots,
            tasks,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::TaskStatus;

    #[test]
    fn reads_everything_and_counts_since_midnight() {
        let (control_plane, clock) = fleet();
        let fleet = run(RefreshFleet::new(control_plane.clone(), clock).execute()).unwrap();
        assert_eq!(fleet.nodes.len(), 2);
        assert_eq!(fleet.nodes[0].id.as_str(), "gx10-ff95");
        assert_eq!(fleet.bots.len(), 3);
        assert_eq!(fleet.bots[0].id.as_str(), "bot");
        assert_eq!(fleet.tasks.len(), 5);
        assert_eq!(fleet.overview.tasks_in(TaskStatus::WaitingApproval), 1);
        assert_eq!(fleet.overview.tasks_since_in(TaskStatus::Failed), 1);
        assert_eq!(fleet.overview.tasks_since_in(TaskStatus::Expired), 1);
        assert_eq!(fleet.overview.since.unwrap().to_rfc3339(), "2026-10-25T00:00:00+00:00");
        assert_eq!(fleet.waiting().len(), 1);
        assert_eq!(fleet.active().len(), 1);
        assert!(control_plane.calls().contains(&"overview".to_string()));
    }

    #[test]
    fn a_failure_of_any_read_fails_the_refresh() {
        let (control_plane, clock) = fleet();
        control_plane.fail_with(Some(PanelError::Unreachable("down".into())));
        let error = run(RefreshFleet::new(control_plane, clock).execute()).unwrap_err();
        assert_eq!(error, PanelError::Unreachable("down".into()));
    }
}
