use serde::{Deserialize, Serialize};

/// How bad an incident is, sev1 the worst. The control plane reads an
/// absent one as sev3, and so does the panel.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum IncidentSeverity {
    Sev1,
    Sev2,
    #[default]
    Sev3,
    Sev4,
    /// A value this panel does not know: the control plane is newer.
    #[serde(other)]
    Unknown,
}

impl IncidentSeverity {
    pub const ALL: [IncidentSeverity; 4] = [Self::Sev1, Self::Sev2, Self::Sev3, Self::Sev4];

    /// The word the control plane uses, which is also the one shown.
    pub fn name(self) -> &'static str {
        match self {
            Self::Sev1 => "sev1",
            Self::Sev2 => "sev2",
            Self::Sev3 => "sev3",
            Self::Sev4 => "sev4",
            Self::Unknown => "unknown",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_match_the_api() {
        for severity in IncidentSeverity::ALL {
            let json = serde_json::to_string(&severity).unwrap();
            assert_eq!(json, format!("\"{}\"", severity.name()));
        }
        assert_eq!(
            serde_json::from_str::<IncidentSeverity>("\"sev0\"").unwrap(),
            IncidentSeverity::Unknown
        );
        assert_eq!(IncidentSeverity::default(), IncidentSeverity::Sev3);
    }
}
