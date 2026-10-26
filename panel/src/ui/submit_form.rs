use crate::domain::{BotId, IdempotencyKey};

/// The form that sends a task to a bot. It keeps one key from the moment
/// it opens: sending twice names the same task.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct SubmitForm {
    pub open: bool,
    pub bot: Option<BotId>,
    pub objective: String,
    pub key: Option<IdempotencyKey>,
    pub busy: bool,
}

impl SubmitForm {
    pub fn opened(bot: Option<BotId>, key: IdempotencyKey) -> Self {
        Self {
            open: true,
            bot,
            objective: String::new(),
            key: Some(key),
            busy: false,
        }
    }

    /// Whether it can be sent: a bot, some words, not already going.
    pub fn ready(&self) -> bool {
        self.bot.is_some() && !self.objective.trim().is_empty() && !self.busy
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_ready_with_a_bot_and_words() {
        let mut form = SubmitForm::opened(None, IdempotencyKey::new("k").unwrap());
        assert!(!form.ready());
        form.bot = Some(BotId::new("bot").unwrap());
        assert!(!form.ready());
        form.objective = "Count".into();
        assert!(form.ready());
        form.busy = true;
        assert!(!form.ready());
    }
}
