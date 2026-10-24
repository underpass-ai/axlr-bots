use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::Bot;

/// The bots, in id order.
pub struct ListBots {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListBots {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self) -> Result<Vec<Bot>, PanelError> {
        let mut bots = self.control_plane.list_bots().await?;
        bots.sort_by(|a, b| a.id.cmp(&b.id));
        Ok(bots)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::application::{PatchBot, ShowBot, SwitchBot};
    use crate::domain::{AutonomyLevel, BotId, BotPatch, BotStatus};

    #[test]
    fn lists_shows_switches_and_patches_bots() {
        let (control_plane, _) = fleet();
        let bots = run(ListBots::new(control_plane.clone()).execute()).unwrap();
        assert_eq!(bots.len(), 3);
        let id = BotId::new("bot").unwrap();
        let switch = SwitchBot::new(control_plane.clone());
        assert_eq!(run(switch.pause(id.clone())).unwrap().status, BotStatus::Paused);
        assert!(matches!(run(switch.pause(id.clone())), Err(PanelError::State(_))));
        assert_eq!(run(switch.enable(id.clone())).unwrap().status, BotStatus::Enabled);
        let patch = BotPatch {
            level: Some(AutonomyLevel::Propose),
            ..Default::default()
        };
        let patched = run(PatchBot::new(control_plane.clone()).execute(id.clone(), patch)).unwrap();
        assert_eq!(patched.level, AutonomyLevel::Propose);
        assert!(matches!(
            run(PatchBot::new(control_plane.clone()).execute(id.clone(), BotPatch::default())),
            Err(PanelError::Invalid(_))
        ));
        let (bot, events) = run(ShowBot::new(control_plane.clone()).execute(id)).unwrap();
        assert_eq!(bot.level, AutonomyLevel::Propose);
        let kinds: Vec<&str> = events.iter().map(|e| e.event_type.as_str()).collect();
        assert!(
            kinds.ends_with(&["bot.paused", "bot.enabled", "bot.updated"]),
            "{kinds:?}"
        );
        let ghost = BotId::new("ghost").unwrap();
        assert!(matches!(
            run(ShowBot::new(control_plane).execute(ghost)),
            Err(PanelError::NotFound(_))
        ));
    }
}
