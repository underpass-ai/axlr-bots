use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{DomainError, IdempotencyKey, Publication, Published, validate_topic};

/// Publishes the operator's event on a team: a name, one line, and whom
/// it is for (the whole team when nobody is named).
pub struct PublishEvent {
    control_plane: Arc<dyn ControlPlane>,
}

impl PublishEvent {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(
        &self,
        team: String,
        name: &str,
        message: &str,
        to: &str,
        key: IdempotencyKey,
    ) -> Result<Published, PanelError> {
        let name = name.trim();
        validate_topic(name)?;
        if name.contains('*') {
            return Err(DomainError::new("name", "is one name, not a pattern").into());
        }
        let message = message.trim();
        if message.is_empty() {
            return Err(DomainError::new("message", "must not be empty").into());
        }
        let to = to
            .split([',', ' '])
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .map(str::to_string)
            .collect();
        let publication = Publication {
            name: name.to_string(),
            message: message.to_string(),
            to,
            idempotency_key: key.as_str().to_string(),
        };
        self.control_plane.publish_event(team, publication).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn publishes_on_the_team() {
        let (control_plane, _) = fleet();
        control_plane.set_groups("bot", &["port"]);
        control_plane.set_groups("bot-wrx80", &["port"]);
        let key = || IdempotencyKey::new("k-1").unwrap();
        let uc = PublishEvent::new(control_plane.clone());
        let published = run(uc.execute("port".into(), " stop.all ", "Everyone stop", "", key())).unwrap();
        assert!(published.created && published.deliveries.len() == 2);
        let one = run(uc.execute("port".into(), "info.x", "x", "bot", key())).unwrap();
        assert_eq!(one.deliveries.len(), 1);
        assert!(run(uc.execute("port".into(), "error.bot", "x", "", key())).is_err());
        assert!(run(uc.execute("port".into(), "info.*", "x", "", key())).is_err());
        assert!(run(uc.execute("port".into(), "info.x", " ", "", key())).is_err());
        assert!(run(uc.execute("port".into(), "Bad", "x", "", key())).is_err());
    }
}
