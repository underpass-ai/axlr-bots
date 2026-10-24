use std::sync::Arc;

use super::{Clock, ControlPlane, PanelError};
use crate::domain::Overview;

/// The overview since the start of the operator's day.
pub struct GetOverview {
    control_plane: Arc<dyn ControlPlane>,
    clock: Arc<dyn Clock>,
}

impl GetOverview {
    pub fn new(control_plane: Arc<dyn ControlPlane>, clock: Arc<dyn Clock>) -> Self {
        Self { control_plane, clock }
    }

    pub async fn execute(&self) -> Result<Overview, PanelError> {
        self.control_plane.overview(self.clock.start_of_today()).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::NodeStatus;

    #[test]
    fn counts_since_the_start_of_the_day() {
        let (control_plane, clock) = fleet();
        control_plane.set_node_status("wrx80", NodeStatus::Offline);
        let overview = run(GetOverview::new(control_plane, clock.clone()).execute()).unwrap();
        assert_eq!(overview.nodes_in(NodeStatus::Online), 1);
        assert_eq!(overview.nodes_down(), 1);
        assert_eq!(overview.since, Some(clock.start_of_today()));
        assert_eq!(overview.executions_since, 3);
    }
}
