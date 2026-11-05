use std::sync::Arc;

use super::{BotEdit, ControlPlane, PanelError, SavedEdit};
use crate::domain::BotId;

/// Saves the operator's edit of a bot: it reads the bot again first and
/// writes nothing if it changed since the edit opened, so a change made
/// meanwhile (another operator, the bot's own ask) is never overwritten
/// unseen. The control plane holds its own lock for the write itself.
pub struct SaveBotEdit {
    control_plane: Arc<dyn ControlPlane>,
}

impl SaveBotEdit {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: BotId, edit: BotEdit) -> Result<SavedEdit, PanelError> {
        let current = self.control_plane.get_bot(id.clone()).await?;
        if current.updated_at != edit.base {
            return Ok(SavedEdit::ChangedMeanwhile(current));
        }
        match edit.patch(&current)? {
            None => Ok(SavedEdit::Unchanged(current)),
            Some(patch) => Ok(SavedEdit::Saved(self.control_plane.patch_bot(id, patch).await?)),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::ToolAccess;

    fn opened(control_plane: &Arc<crate::adapters::fake::FakeControlPlane>, id: &BotId) -> BotEdit {
        BotEdit::of(&run(control_plane.get_bot(id.clone())).unwrap())
    }

    #[test]
    fn saves_the_tools_and_the_mission_in_one_patch() {
        let (control_plane, _) = fleet();
        let id = BotId::new("bot-wrx80").unwrap();
        let mut edit = opened(&control_plane, &id);
        edit.tools.add("local_search", ToolAccess::OnItsOwn).unwrap();
        edit.tools.set(2, ToolAccess::Never);
        edit.mission = "  Watch the disks.  ".into();
        let saved = run(SaveBotEdit::new(control_plane.clone()).execute(id.clone(), edit)).unwrap();
        let SavedEdit::Saved(bot) = saved else {
            panic!("not saved: {saved:?}")
        };
        assert_eq!(bot.mission, "Watch the disks.");
        assert!(bot.allowed_tools.contains(&"local_search".to_string()));
        assert_eq!(bot.approvals.auto_approve[0].tool, "local_search");
        assert_eq!(bot.tool_rules.deny[0].tool, "local_write");
        let calls = control_plane.calls();
        assert_eq!(calls.iter().filter(|c| c.starts_with("patch_bot")).count(), 1);
    }

    #[test]
    fn writes_nothing_over_a_change_made_meanwhile_or_when_nothing_changed() {
        let (control_plane, _) = fleet();
        let id = BotId::new("bot-wrx80").unwrap();
        let unchanged = run(SaveBotEdit::new(control_plane.clone()).execute(id.clone(), opened(&control_plane, &id)));
        assert!(matches!(unchanged, Ok(SavedEdit::Unchanged(_))));
        let mut edit = opened(&control_plane, &id);
        edit.tools.remove(0);
        control_plane.change_bot("bot-wrx80", |bot| bot.allowed_tools.push("local_exec".into()));
        let saved = run(SaveBotEdit::new(control_plane.clone()).execute(id.clone(), edit)).unwrap();
        let SavedEdit::ChangedMeanwhile(now) = saved else {
            panic!("overwrote: {saved:?}")
        };
        assert!(now.allowed_tools.contains(&"local_exec".to_string()));
        assert!(!control_plane.calls().iter().any(|c| c.starts_with("patch_bot")));
        // An empty mission is refused before anything is written.
        let mut empty = opened(&control_plane, &id);
        empty.mission = "  ".into();
        assert!(matches!(
            run(SaveBotEdit::new(control_plane.clone()).execute(id, empty)),
            Err(PanelError::Invalid(_))
        ));
    }
}
