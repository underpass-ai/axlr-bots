use std::fmt;

use crate::domain::DomainError;

/// Why something the operator asked for did not happen, sorted so the
/// panel can say it in a few words.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PanelError {
    /// The control plane refused the operator token.
    Unauthorized(String),
    /// The control plane refused the act.
    Forbidden(String),
    NotFound(String),
    /// A value the operator gave, or the control plane read, breaks a rule.
    Invalid(String),
    /// The same thing exists already.
    Conflict(String),
    /// The resource is not in a state that allows it.
    State(String),
    /// The control plane failed, or answered something unexpected.
    Server(String),
    /// The control plane could not be reached: down, wrong address, TLS.
    Unreachable(String),
    /// The answer could not be read.
    Unreadable(String),
    /// The panel's settings, the CA or the token file are missing or bad.
    Config(String),
    /// A local file could not be read.
    File(String),
}

impl PanelError {
    /// Two or three words for the banner.
    pub fn title(&self) -> &'static str {
        match self {
            Self::Unauthorized(_) => "Wrong token",
            Self::Forbidden(_) => "Not allowed",
            Self::NotFound(_) => "Not found",
            Self::Invalid(_) => "Check the input",
            Self::Conflict(_) => "Already there",
            Self::State(_) => "Not now",
            Self::Server(_) => "Control plane error",
            Self::Unreachable(_) => "Cannot connect",
            Self::Unreadable(_) => "Bad answer",
            Self::Config(_) => "Check settings",
            Self::File(_) => "Cannot read file",
        }
    }

    /// The control plane's or the system's own words.
    pub fn detail(&self) -> &str {
        match self {
            Self::Unauthorized(d)
            | Self::Forbidden(d)
            | Self::NotFound(d)
            | Self::Invalid(d)
            | Self::Conflict(d)
            | Self::State(d)
            | Self::Server(d)
            | Self::Unreachable(d)
            | Self::Unreadable(d)
            | Self::Config(d)
            | Self::File(d) => d,
        }
    }

    /// Whether the settings screen is the way out.
    pub fn needs_settings(&self) -> bool {
        matches!(self, Self::Unauthorized(_) | Self::Unreachable(_) | Self::Config(_))
    }
}

impl fmt::Display for PanelError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.title(), self.detail())
    }
}

impl std::error::Error for PanelError {}

impl From<DomainError> for PanelError {
    fn from(error: DomainError) -> Self {
        Self::Invalid(error.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_kind_has_a_title_and_its_detail() {
        let all = [
            PanelError::Unauthorized("a".into()),
            PanelError::Forbidden("a".into()),
            PanelError::NotFound("a".into()),
            PanelError::Invalid("a".into()),
            PanelError::Conflict("a".into()),
            PanelError::State("a".into()),
            PanelError::Server("a".into()),
            PanelError::Unreachable("a".into()),
            PanelError::Unreadable("a".into()),
            PanelError::Config("a".into()),
            PanelError::File("a".into()),
        ];
        let mut titles: Vec<&str> = all.iter().map(PanelError::title).collect();
        titles.dedup();
        assert_eq!(titles.len(), all.len());
        assert!(all.iter().all(|e| e.detail() == "a"));
        assert!(all[0].needs_settings() && all[7].needs_settings() && all[9].needs_settings());
        assert!(!all[2].needs_settings());
        assert_eq!(all[2].to_string(), "Not found: a");
    }

    #[test]
    fn a_domain_error_is_invalid_input() {
        let error: PanelError = DomainError::new("objective", "must not be empty").into();
        assert_eq!(error, PanelError::Invalid("objective must not be empty".into()));
    }
}
