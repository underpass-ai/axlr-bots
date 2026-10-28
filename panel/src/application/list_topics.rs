use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::Topic;

/// A team's topics, or `None` when the control plane is older than the
/// topics route: the panel says so and nothing else breaks.
pub struct ListTopics {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListTopics {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, team: String) -> Result<Option<Vec<Topic>>, PanelError> {
        match self.control_plane.group_topics(team).await {
            Ok(topics) => Ok(Some(topics)),
            Err(PanelError::NotFound(detail)) if detail.contains("404 page not found") => Ok(None),
            Err(error) => Err(error),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn an_older_control_plane_has_no_topics() {
        let (control_plane, _) = fleet();
        control_plane.set_groups("bot", &["port"]);
        assert_eq!(
            run(ListTopics::new(control_plane.clone()).execute("port".into())).unwrap(),
            Some(Vec::new())
        );
        assert!(run(ListTopics::new(control_plane.clone()).execute("sea".into())).is_err());
        control_plane.without_newer_routes();
        assert_eq!(
            run(ListTopics::new(control_plane).execute("port".into())).unwrap(),
            None
        );
    }
}
