use std::fmt;

/// A value the control plane or the operator gave that breaks a rule of
/// the domain: the field and why.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DomainError {
    pub field: &'static str,
    pub reason: String,
}

impl DomainError {
    pub fn new(field: &'static str, reason: impl Into<String>) -> Self {
        Self {
            field,
            reason: reason.into(),
        }
    }
}

impl fmt::Display for DomainError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} {}", self.field, self.reason)
    }
}

impl std::error::Error for DomainError {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn displays_the_field_and_the_reason() {
        let error = DomainError::new("bot id", "must not be empty");
        assert_eq!(error.to_string(), "bot id must not be empty");
    }
}
