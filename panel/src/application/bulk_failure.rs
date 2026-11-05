use super::PanelError;

/// A row a bulk act did not go through for, and why, in the control
/// plane's words.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkFailure {
    pub name: String,
    pub reason: String,
}

impl BulkFailure {
    pub fn new(name: impl Into<String>, error: &PanelError) -> Self {
        let reason = match error.detail().trim() {
            "" => error.title().to_string(),
            detail => detail.to_string(),
        };
        Self {
            name: name.into(),
            reason,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_the_control_planes_words_or_the_errors_title() {
        let failure = BulkFailure::new("worker-1", &PanelError::State("bot is paused".into()));
        assert_eq!(
            (failure.name.as_str(), failure.reason.as_str()),
            ("worker-1", "bot is paused")
        );
        assert_eq!(
            BulkFailure::new("w", &PanelError::Server(" ".into())).reason,
            "Control plane error"
        );
    }
}
