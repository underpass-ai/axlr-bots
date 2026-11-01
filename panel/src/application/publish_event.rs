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
        data: Option<serde_json::Value>,
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
            data,
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
        control_plane.set_groups("bot", &["ops"]);
        control_plane.set_groups("bot-wrx80", &["ops"]);
        let key = || IdempotencyKey::new("k-1").unwrap();
        let uc = PublishEvent::new(control_plane.clone());
        let published = run(uc.execute("ops".into(), " stop.all ", "Everyone stop", "", None, key())).unwrap();
        assert!(published.created && published.deliveries.len() == 2);
        let one = run(uc.execute("ops".into(), "info.x", "x", "bot", None, key())).unwrap();
        assert_eq!(one.deliveries.len(), 1);
        assert!(run(uc.execute("ops".into(), "error.bot", "x", "", None, key())).is_err());
        assert!(run(uc.execute("ops".into(), "info.*", "x", "", None, key())).is_err());
        assert!(run(uc.execute("ops".into(), "info.x", " ", "", None, key())).is_err());
        assert!(run(uc.execute("ops".into(), "Bad", "x", "", None, key())).is_err());
    }
}
