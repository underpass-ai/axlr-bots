use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Bot, BotId};

/// Updates a bot to the version of its definition staged on the control
/// plane (`POST /v1/bots/{id}/staged/apply`): the control plane runs the
/// staged change through its update, with the same checks, and answers
/// the bot at its new version.
pub struct ApplyStaged {
    control_plane: Arc<dyn ControlPlane>,
}

impl ApplyStaged {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    /// Applies the staged change. Nothing staged any more (applied or
    /// dropped since the screen was read) is a state error that says so;
    /// a change the bot no longer takes comes back as the control plane
    /// refused it, and stays staged.
    pub async fn execute(&self, id: BotId) -> Result<Bot, PanelError> {
        self.control_plane
            .apply_staged(id.clone())
            .await
            .map_err(|error| match error {
                PanelError::State(detail) => PanelError::State(format!(
                    "{id} has no staged version to apply now: it was applied or dropped meanwhile ({detail})"
                )),
                other => other,
            })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn updates_the_bot_to_the_staged_version() {
        let (control_plane, _) = fleet();
        control_plane.change_bot("bot", |b| b.version = "1.2.0".into());
        control_plane.stage_bot("bot", "1.3.0");
        let id = BotId::new("bot").unwrap();
        let bot = run(ApplyStaged::new(control_plane.clone()).execute(id.clone())).unwrap();
        assert_eq!((bot.version.as_str(), bot.staged.is_none()), ("1.3.0", true));
        assert!(bot.update().is_none());
        let events = run(control_plane.bot_events(id, 10)).unwrap();
        let kinds: Vec<&str> = events.iter().map(|e| e.event_type.as_str()).collect();
        assert!(
            kinds.ends_with(&["bot.staged", "bot.updated", "bot.staged.applied"]),
            "{kinds:?}"
        );
    }

    #[test]
    fn nothing_staged_says_so_and_other_errors_pass_on() {
        let (control_plane, _) = fleet();
        let id = BotId::new("bot").unwrap();
        let error = run(ApplyStaged::new(control_plane.clone()).execute(id.clone())).unwrap_err();
        assert!(
            matches!(&error, PanelError::State(text) if text.contains("bot has no staged version") && text.contains("meanwhile")),
            "{error:?}"
        );
        assert_eq!(error.title(), "Not now");
        let ghost = run(ApplyStaged::new(control_plane.clone()).execute(BotId::new("ghost").unwrap())).unwrap_err();
        assert!(matches!(ghost, PanelError::NotFound(_)));
        control_plane.fail_with(Some(PanelError::Unreachable("down".into())));
        let down = run(ApplyStaged::new(control_plane).execute(id)).unwrap_err();
        assert!(matches!(down, PanelError::Unreachable(_)));
    }
}
