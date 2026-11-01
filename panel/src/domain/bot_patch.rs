use serde::Serialize;

use super::identifier::{validate_identifier, validate_text};
use super::{AutonomyLevel, DomainError, MAX_SUBSCRIPTIONS, Placement, Subscription};

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
    /// The bot's teams, the whole new list.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub groups: Option<Vec<String>>,
    /// The topics it listens to, the whole new list.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub subscriptions: Option<Vec<Subscription>>,
}

impl BotPatch {
    pub fn is_empty(&self) -> bool {
        self.mission.is_none()
            && self.level.is_none()
            && self.allowed_tools.is_none()
            && self.placement.is_none()
            && self.groups.is_none()
            && self.subscriptions.is_none()
    }

    /// A patch must name a field, and a new mission must be valid.
    pub fn validate(&self) -> Result<(), DomainError> {
        if self.is_empty() {
            return Err(DomainError::new("patch", "must change at least one field"));
        }
        if let Some(mission) = &self.mission {
            validate_text("mission", mission, 2000)?;
        }
        if let Some(subscriptions) = &self.subscriptions
            && subscriptions.len() > MAX_SUBSCRIPTIONS
        {
            return Err(DomainError::new("subscriptions", "a bot listens to at most 16 topics"));
        }
        if let Some(groups) = &self.groups {
            if groups.len() > 8 {
                return Err(DomainError::new("team", "a bot is in at most 8 teams"));
            }
            for team in groups {
                validate_identifier("team", team)?;
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_patch_of_teams() {
        let teams = BotPatch {
            groups: Some(vec!["ops".into()]),
            ..BotPatch::default()
        };
        assert!(!teams.is_empty() && teams.validate().is_ok());
        assert_eq!(serde_json::to_string(&teams).unwrap(), r#"{"groups":["ops"]}"#);
        let none = BotPatch {
            groups: Some(Vec::new()),
            ..BotPatch::default()
        };
        assert!(none.validate().is_ok());
        let bad = BotPatch {
            groups: Some(vec!["Bad Name".into()]),
            ..BotPatch::default()
        };
        assert!(bad.validate().is_err());
        let many = BotPatch {
            groups: Some((0..9).map(|i| format!("t{i}")).collect()),
            ..BotPatch::default()
        };
        assert!(many.validate().is_err());
    }

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
