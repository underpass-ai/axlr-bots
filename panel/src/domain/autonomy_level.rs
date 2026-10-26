use serde::{Deserialize, Serialize};

/// How far a bot may act on its own: from only reading to acting within
/// its policy without asking.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub enum AutonomyLevel {
    #[default]
    Observe,
    Propose,
    ActWithApproval,
    ActWithinPolicy,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    Unknown,
}

impl AutonomyLevel {
    pub const ALL: [AutonomyLevel; 4] = [
        Self::Observe,
        Self::Propose,
        Self::ActWithApproval,
        Self::ActWithinPolicy,
    ];

    /// The control plane's word.
    pub fn name(self) -> &'static str {
        match self {
            Self::Observe => "observe",
            Self::Propose => "propose",
            Self::ActWithApproval => "act-with-approval",
            Self::ActWithinPolicy => "act-within-policy",
            Self::Unknown => "unknown",
        }
    }

    /// The short words shown.
    pub fn label(self) -> &'static str {
        match self {
            Self::Observe => "Observe",
            Self::Propose => "Propose",
            Self::ActWithApproval => "Ask first",
            Self::ActWithinPolicy => "Act",
            Self::Unknown => "Unknown",
        }
    }

    /// The control plane reads an empty level as observe.
    pub fn parse(raw: &str) -> Option<Self> {
        if raw.is_empty() {
            return Some(Self::Observe);
        }
        Self::ALL.into_iter().find(|l| l.name() == raw)
    }

    /// Whether the bot may write, with or without asking.
    pub fn writes(self) -> bool {
        matches!(self, Self::ActWithApproval | Self::ActWithinPolicy)
    }
}

impl TryFrom<String> for AutonomyLevel {
    type Error = super::DomainError;

    /// The control plane may name a level this panel does not know yet.
    /// A bot definition refuses it when it is checked.
    fn try_from(raw: String) -> Result<Self, Self::Error> {
        Ok(Self::parse(&raw).unwrap_or(Self::Unknown))
    }
}

impl From<AutonomyLevel> for String {
    fn from(level: AutonomyLevel) -> Self {
        level.name().to_string()
    }
}

impl std::fmt::Display for AutonomyLevel {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.label())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_the_control_plane_words() {
        for level in AutonomyLevel::ALL {
            assert_eq!(AutonomyLevel::parse(level.name()), Some(level));
            assert_eq!(serde_json::to_string(&level).unwrap(), format!("\"{}\"", level.name()));
        }
        assert_eq!(AutonomyLevel::parse(""), Some(AutonomyLevel::Observe));
        assert_eq!(
            serde_json::from_str::<AutonomyLevel>("\"yolo\"").unwrap(),
            AutonomyLevel::Unknown
        );
        assert!(AutonomyLevel::ActWithApproval.writes());
        assert!(!AutonomyLevel::Propose.writes());
        assert_eq!(AutonomyLevel::ActWithApproval.to_string(), "Ask first");
    }
}
