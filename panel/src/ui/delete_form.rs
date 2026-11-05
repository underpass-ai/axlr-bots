use crate::application::DeletePreview;
use crate::domain::BotId;

/// The Delete confirmation on a bot's page: what deleting it will do,
/// once read, and the bot's id typed again, the deliberate act it needs.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct DeleteForm {
    pub bot: Option<BotId>,
    pub preview: Option<DeletePreview>,
    pub typed: String,
    pub busy: bool,
    pub error: Option<String>,
}

impl DeleteForm {
    pub fn opened(bot: BotId) -> Self {
        Self {
            bot: Some(bot),
            ..Self::default()
        }
    }

    pub fn is_open_for(&self, bot: &BotId) -> bool {
        self.bot.as_ref() == Some(bot)
    }

    /// Whether the delete may go: the preview read, the id typed as it
    /// is, nothing on its way.
    pub fn ready(&self) -> bool {
        !self.busy && self.preview.is_some() && self.bot.as_ref().is_some_and(|b| self.typed.trim() == b.as_str())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn goes_only_once_read_and_typed() {
        let bot = BotId::new("worker-1").unwrap();
        let mut form = DeleteForm::opened(bot.clone());
        assert!(form.is_open_for(&bot) && !form.ready());
        form.typed = "worker-1".into();
        assert!(!form.ready(), "the preview is not read yet");
        form.preview = Some(DeletePreview::default());
        assert!(form.ready());
        form.typed = "worker-2".into();
        assert!(!form.ready());
        form.typed = " worker-1 ".into();
        form.busy = true;
        assert!(!form.ready());
        assert!(!DeleteForm::default().ready());
    }
}
