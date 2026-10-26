use serde::{Deserialize, Serialize};

/// Where a node stands: registered and not yet heard, heartbeating, silent,
/// or cut off.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum NodeStatus {
    Registered,
    Online,
    Offline,
    Revoked,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    #[serde(other)]
    Unknown,
}

impl NodeStatus {
    pub const ALL: [NodeStatus; 4] = [Self::Registered, Self::Online, Self::Offline, Self::Revoked];

    /// The word the control plane uses, which is also the one shown.
    pub fn name(self) -> &'static str {
        match self {
            Self::Registered => "Registered",
            Self::Online => "Online",
            Self::Offline => "Offline",
            Self::Revoked => "Revoked",
            Self::Unknown => "Unknown",
        }
    }

    /// Whether the node can be revoked from here.
    pub fn can_revoke(self) -> bool {
        self != Self::Revoked
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_match_the_api() {
        for status in NodeStatus::ALL {
            let json = serde_json::to_string(&status).unwrap();
            assert_eq!(json, format!("\"{}\"", status.name()));
        }
        assert_eq!(
            serde_json::from_str::<NodeStatus>("\"Lost\"").unwrap(),
            NodeStatus::Unknown
        );
        assert!(NodeStatus::Online.can_revoke());
        assert!(!NodeStatus::Revoked.can_revoke());
    }
}
