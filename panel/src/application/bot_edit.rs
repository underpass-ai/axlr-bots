use chrono::{DateTime, Utc};

use crate::domain::{Bot, BotPatch, DomainError, ToolPlan};

/// What the operator changed on a bot's page: its mission and its tools,
/// made on the bot as it was at `base` (its `updatedAt` when the edit
/// opened).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BotEdit {
    pub base: DateTime<Utc>,
    pub mission: String,
    pub tools: ToolPlan,
}

impl BotEdit {
    /// The edit of the bot as it is now.
    pub fn of(bot: &Bot) -> Self {
        Self {
            base: bot.updated_at,
            mission: bot.mission.clone(),
            tools: ToolPlan::of(bot),
        }
    }

    /// The patch that makes `current` look like the edit; `None` when it
    /// does already.
    pub fn patch(&self, current: &Bot) -> Result<Option<BotPatch>, DomainError> {
        let mut patch = self.tools.patch(current).unwrap_or_default();
        let mission = self.mission.trim();
        if mission != current.mission {
            patch.mission = Some(mission.to_string());
        }
        if patch.is_empty() {
            return Ok(None);
        }
        patch.validate()?;
        Ok(Some(patch))
    }
}
