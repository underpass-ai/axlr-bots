use serde::{Deserialize, Serialize};

/// Where an incident stands: open and being triaged, diagnosed, a fix
/// proposed, being applied after a person approved it, applied and being
/// verified, closed, or handed to a person.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum IncidentStatus {
    Open,
    Diagnosed,
    /// A fix and its rollback proposed, applied by nobody yet; its apply
    /// task may wait for a person's approval.
    Proposed,
    /// A person approved the apply: the fix is being carried out.
    Applying,
    /// The fix was applied: a verify task checks it, the alerts going
    /// green close it.
    Verifying,
    Resolved,
    Escalated,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    #[serde(other)]
    Unknown,
}

impl IncidentStatus {
    pub const ALL: [IncidentStatus; 7] = [
        Self::Open,
        Self::Diagnosed,
        Self::Proposed,
        Self::Applying,
        Self::Verifying,
        Self::Resolved,
        Self::Escalated,
    ];

    /// The word the control plane uses, which is also the one shown.
    pub fn name(self) -> &'static str {
        match self {
            Self::Open => "Open",
            Self::Diagnosed => "Diagnosed",
            Self::Proposed => "Proposed",
            Self::Applying => "Applying",
            Self::Verifying => "Verifying",
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

    /// Whether it stands on a proposal: Proposed, Applying or Verifying.
    pub fn has_proposal(self) -> bool {
        matches!(self, Self::Proposed | Self::Applying | Self::Verifying)
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
        assert!(IncidentStatus::Applying.has_proposal() && IncidentStatus::Verifying.has_proposal());
        assert!(!IncidentStatus::Open.has_proposal());
    }
}
