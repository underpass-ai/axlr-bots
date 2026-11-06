use serde::{Deserialize, Serialize};

/// The step of an incident a task works on: triage finds the cause, fix
/// proposes the change and its rollback.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum IncidentPhase {
    #[default]
    Triage,
    Fix,
    /// A value this panel does not know: the control plane is newer.
    #[serde(other)]
    Unknown,
}

impl IncidentPhase {
    pub const ALL: [IncidentPhase; 2] = [Self::Triage, Self::Fix];

    /// The word the control plane uses, which is also the one shown.
    pub fn name(self) -> &'static str {
        match self {
            Self::Triage => "triage",
            Self::Fix => "fix",
            Self::Unknown => "unknown",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_match_the_api() {
        for phase in IncidentPhase::ALL {
            let json = serde_json::to_string(&phase).unwrap();
            assert_eq!(json, format!("\"{}\"", phase.name()));
        }
        assert_eq!(
            serde_json::from_str::<IncidentPhase>("\"verify\"").unwrap(),
            IncidentPhase::Unknown
        );
        assert_eq!(IncidentPhase::Unknown.name(), "unknown");
    }
}
