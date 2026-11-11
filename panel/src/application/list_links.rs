use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::TeamLink;

/// A team's links in the team channel, or `None` when the control plane
/// is older than the links route: the panel says so and nothing else
/// breaks.
pub struct ListLinks {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListLinks {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, team: String) -> Result<Option<Vec<TeamLink>>, PanelError> {
        match self.control_plane.group_links(team).await {
            Ok(links) => Ok(Some(links)),
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
    fn reads_the_links_set_and_an_older_control_plane_has_none() {
        let (control_plane, _) = fleet();
        control_plane.set_groups("bot", &["ops"]);
        assert_eq!(
            run(ListLinks::new(control_plane.clone()).execute("ops".into())).unwrap(),
            Some(Vec::new())
        );
        let link = TeamLink {
            from: "ops".into(),
            to: "dev".into(),
            askers: vec!["bot".into()],
            responders: vec!["dev-a".into()],
        };
        control_plane.set_links("ops", vec![link.clone()]);
        assert_eq!(
            run(ListLinks::new(control_plane.clone()).execute("ops".into())).unwrap(),
            Some(vec![link])
        );
        assert!(run(ListLinks::new(control_plane.clone()).execute("qa".into())).is_err());
        control_plane.without_newer_routes();
        assert_eq!(run(ListLinks::new(control_plane).execute("ops".into())).unwrap(), None);
    }
}
