use serde::{Deserialize, Serialize};

/// Where an incident stands: open and being triaged, diagnosed, closed,
/// or handed to a person.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum IncidentStatus {
    Open,
    Diagnosed,
    Resolved,
    Escalated,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    #[serde(other)]
    Unknown,
}

impl IncidentStatus {
    pub const ALL: [IncidentStatus; 4] = [Self::Open, Self::Diagnosed, Self::Resolved, Self::Escalated];

    /// The word the control plane uses, which is also the one shown.
    pub fn name(self) -> &'static str {
        match self {
            Self::Open => "Open",
            Self::Diagnosed => "Diagnosed",
            Self::Resolved => "Resolved",
            Self::Escalated => "Escalated",
            Self::Unknown => "Unknown",
        }
    }

    /// Whether the incident can be resolved from here: the control plane
    /// refuses one already resolved.
    pub fn can_resolve(self) -> bool {
        self != Self::Resolved
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_match_the_api() {
        for status in IncidentStatus::ALL {
            let json = serde_json::to_string(&status).unwrap();
            assert_eq!(json, format!("\"{}\"", status.name()));
        }
        assert_eq!(
            serde_json::from_str::<IncidentStatus>("\"Muted\"").unwrap(),
            IncidentStatus::Unknown
        );
        assert_eq!(IncidentStatus::Unknown.name(), "Unknown");
        assert!(IncidentStatus::Escalated.can_resolve());
        assert!(!IncidentStatus::Resolved.can_resolve());
    }
}
