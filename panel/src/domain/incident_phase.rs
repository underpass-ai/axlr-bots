use serde::{Deserialize, Serialize};

/// The step of an incident a task works on: triage finds the cause, fix
/// proposes the change and its rollback, apply carries it out once a
/// person approves its call, verify checks it took, postmortem writes
/// down a resolved incident.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum IncidentPhase {
    #[default]
    Triage,
    Fix,
    Apply,
    Verify,
    Postmortem,
    /// A value this panel does not know: the control plane is newer.
    #[serde(other)]
    Unknown,
}

impl IncidentPhase {
    pub const ALL: [IncidentPhase; 5] = [Self::Triage, Self::Fix, Self::Apply, Self::Verify, Self::Postmortem];

    /// The word the control plane uses, which is also the one shown.
    pub fn name(self) -> &'static str {
        match self {
            Self::Triage => "triage",
            Self::Fix => "fix",
            Self::Apply => "apply",
            Self::Verify => "verify",
            Self::Postmortem => "postmortem",
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
            serde_json::from_str::<IncidentPhase>("\"rollback\"").unwrap(),
            IncidentPhase::Unknown
        );
        assert_eq!(IncidentPhase::Unknown.name(), "unknown");
    }
}
