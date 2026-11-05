use crate::application::BotEdit;
use crate::domain::{Bot, BotId, ToolAccess};

/// The Edit part of a bot's page: the mission and the tools as the
/// operator changes them, the tool being added, and what the last save
/// said.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BotEditForm {
    pub bot: Option<BotId>,
    pub draft: Option<BotEdit>,
    pub new_tool: String,
    pub new_access: ToolAccess,
    pub busy: bool,
    /// Why the last add or save did not go through, in words.
    pub error: Option<String>,
}

impl Default for BotEditForm {
    fn default() -> Self {
        Self {
            bot: None,
            draft: None,
            new_tool: String::new(),
            new_access: ToolAccess::AsksFirst,
            busy: false,
            error: None,
        }
    }
}

impl BotEditForm {
    /// Open on the bot as it is now.
    pub fn opened(bot: &Bot) -> Self {
        Self {
            bot: Some(bot.id.clone()),
            draft: Some(BotEdit::of(bot)),
            ..Self::default()
        }
    }

    pub fn is_open_for(&self, bot: &BotId) -> bool {
        self.bot.as_ref() == Some(bot)
    }

    /// Adds the tool typed, or the one named, with the access picked.
    pub fn add(&mut self, name: Option<&str>) {
        let Some(draft) = self.draft.as_mut() else {
            return;
        };
        let name = name.map(str::to_string).unwrap_or_else(|| self.new_tool.clone());
        match draft.tools.add(&name, self.new_access) {
            Ok(()) => {
                self.new_tool.clear();
                self.error = None;
            }
            Err(error) => self.error = Some(error.to_string()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bot() -> Bot {
        serde_json::from_str(
            r#"{"id":"worker-1","name":"worker-1","mission":"m","status":"Enabled","allowedTools":["local_read"],
            "createdAt":"2026-11-05T10:00:00Z","updatedAt":"2026-11-05T10:00:00Z"}"#,
        )
        .unwrap()
    }

    #[test]
    fn adds_the_tool_typed_or_named_and_says_why_not() {
        let mut form = BotEditForm::opened(&bot());
        assert!(form.is_open_for(&BotId::new("worker-1").unwrap()));
        form.new_tool = "local_list".into();
        form.new_access = ToolAccess::OnItsOwn;
        form.add(None);
        assert!(form.new_tool.is_empty() && form.error.is_none());
        form.add(Some("local_list"));
        assert_eq!(form.error.as_deref(), Some("tool local_list is listed already"));
        form.add(Some("docdb.find"));
        let tools = &form.draft.as_ref().unwrap().tools.tools;
        assert_eq!(tools.len(), 3);
        assert_eq!(tools[1].access, ToolAccess::OnItsOwn);
        let mut closed = BotEditForm::default();
        closed.add(Some("local_list"));
        assert!(closed.draft.is_none());
    }
}
