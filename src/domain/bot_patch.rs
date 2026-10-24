use serde::Serialize;

use super::identifier::validate_text;
use super::{AutonomyLevel, DomainError, Placement};

/// The fields of a bot to replace; the others stay. Each named field is
/// the whole new value.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BotPatch {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mission: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<AutonomyLevel>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub allowed_tools: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub placement: Option<Placement>,
}

impl BotPatch {
    pub fn is_empty(&self) -> bool {
        self.mission.is_none()
            && self.level.is_none()
            && self.allowed_tools.is_none()
            && self.placement.is_none()
    }

    /// A patch must name a field, and a new mission must be valid.
    pub fn validate(&self) -> Result<(), DomainError> {
        if self.is_empty() {
            return Err(DomainError::new("patch", "must change at least one field"));
        }
        if let Some(mission) = &self.mission {
            validate_text("mission", mission, 2000)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn serializes_only_what_changes() {
        let patch = BotPatch {
            level: Some(AutonomyLevel::ActWithApproval),
            ..Default::default()
        };
        assert_eq!(
            serde_json::to_string(&patch).unwrap(),
            r#"{"level":"act-with-approval"}"#
        );
        assert!(patch.validate().is_ok());
        assert!(BotPatch::default().validate().is_err());
        let bad = BotPatch {
            mission: Some("  ".into()),
            ..Default::default()
        };
        assert!(bad.validate().is_err());
        assert!(!bad.is_empty());
    }
}
