use serde::{Deserialize, Serialize};

/// Where a bot stands: a draft, registered, accepting tasks, or on hold.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum BotStatus {
    Draft,
    Registered,
    Enabled,
    Paused,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    #[serde(other)]
    Unknown,
}

impl BotStatus {
    pub const ALL: [BotStatus; 4] = [Self::Draft, Self::Registered, Self::Enabled, Self::Paused];

    pub fn name(self) -> &'static str {
        match self {
            Self::Draft => "Draft",
            Self::Registered => "Registered",
            Self::Enabled => "Enabled",
            Self::Paused => "Paused",
            Self::Unknown => "Unknown",
        }
    }

    /// A Registered or Paused bot can be enabled.
    pub fn can_enable(self) -> bool {
        matches!(self, Self::Registered | Self::Paused)
    }

    /// An Enabled bot can be paused.
    pub fn can_pause(self) -> bool {
        self == Self::Enabled
    }

    /// Only an Enabled bot accepts a task.
    pub fn accepts_tasks(self) -> bool {
        self == Self::Enabled
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn transitions_follow_the_control_plane() {
        assert!(BotStatus::Registered.can_enable());
        assert!(BotStatus::Paused.can_enable());
        assert!(!BotStatus::Enabled.can_enable());
        assert!(BotStatus::Enabled.can_pause());
        assert!(!BotStatus::Paused.can_pause());
        assert!(BotStatus::Enabled.accepts_tasks());
        assert!(!BotStatus::Draft.accepts_tasks());
        for status in BotStatus::ALL {
            assert_eq!(
                serde_json::to_string(&status).unwrap(),
                format!("\"{}\"", status.name())
            );
        }
    }
}
