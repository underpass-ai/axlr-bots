use crate::domain::IdempotencyKey;

/// The form that publishes the operator's event on a team. It keeps one
/// key from the moment it opens: sending twice is the same event.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct PublishForm {
    pub team: Option<String>,
    pub name: String,
    pub message: String,
    pub to: String,
    pub key: Option<IdempotencyKey>,
    pub busy: bool,
}

impl PublishForm {
    pub fn opened(team: String, key: IdempotencyKey) -> Self {
        Self {
            team: Some(team),
            key: Some(key),
            ..Self::default()
        }
    }

    pub fn is_open_for(&self, team: &str) -> bool {
        self.team.as_deref() == Some(team)
    }

    /// Whether it can be sent: a name, a message, not already going.
    pub fn ready(&self) -> bool {
        !self.name.trim().is_empty() && !self.message.trim().is_empty() && !self.busy
    }
}
