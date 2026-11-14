use super::definition_mapper::DefinitionMapper;
use super::proto;
use crate::application::PanelError;
use crate::domain::BotPatch;

/// The operator's partial change to a bot, into the wire message. A field
/// the patch does not set is absent; a list set to nothing is an empty
/// wrapper, which clears it.
pub(super) struct PatchMapper;

impl PatchMapper {
    pub fn patch(patch: &BotPatch) -> Result<proto::BotPatch, PanelError> {
        Ok(proto::BotPatch {
            mission: patch.mission.clone(),
            level: patch.level.map(|l| l.name().to_string()),
            allowed_tools: patch.allowed_tools.clone().map(|values| proto::StringList { values }),
            tool_rules: patch.tool_rules.as_ref().map(DefinitionMapper::tool_rules),
            placement: patch.placement.as_ref().map(DefinitionMapper::placement),
            auto_approve: patch.auto_approve.as_ref().map(|rules| proto::ToolRuleList {
                values: DefinitionMapper::rules(rules),
            }),
            groups: patch.groups.clone().map(|values| proto::StringList { values }),
            subscriptions: patch
                .subscriptions
                .as_ref()
                .map(|s| DefinitionMapper::subscriptions(s).map(|values| proto::SubscriptionList { values }))
                .transpose()?,
            ..Default::default()
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{AutonomyLevel, ToolRule};

    #[test]
    fn leaves_alone_what_is_not_set_and_clears_what_is_set_empty() {
        let message = PatchMapper::patch(&BotPatch {
            mission: Some("New.".into()),
            level: Some(AutonomyLevel::Propose),
            groups: Some(Vec::new()),
            auto_approve: Some(vec![ToolRule {
                tool: "local_read".into(),
                arguments: Default::default(),
            }]),
            ..Default::default()
        })
        .unwrap();
        assert_eq!(
            (message.mission.as_deref(), message.level.as_deref()),
            (Some("New."), Some("propose"))
        );
        assert_eq!(message.groups, Some(proto::StringList { values: Vec::new() }));
        assert!(message.allowed_tools.is_none() && message.placement.is_none() && message.subscriptions.is_none());
        assert_eq!(message.auto_approve.unwrap().values[0].tool, "local_read");
        assert!(message.version.is_none() && message.model.is_none());
    }
}
