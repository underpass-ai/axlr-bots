use crate::domain::{BotId, IdempotencyKey};

/// The form that gives a bot an urgent order: the words the operator
/// types become an urgent task, which stops what the bot is doing. It
/// keeps one key from the moment it opens: sending twice is the same
/// order.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct OrderForm {
    pub bot: Option<BotId>,
    pub text: String,
    pub key: Option<IdempotencyKey>,
}

impl OrderForm {
    pub fn opened(bot: BotId, key: IdempotencyKey) -> Self {
        Self {
            bot: Some(bot),
            text: String::new(),
            key: Some(key),
        }
    }

    /// Whether it is open for the bot.
    pub fn is_open_for(&self, bot: &BotId) -> bool {
        self.bot.as_ref() == Some(bot)
    }

    /// Whether it can be sent: an order in words.
    pub fn ready(&self) -> bool {
        !self.text.trim().is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_ready_with_words() {
        let bot = BotId::new("bot").unwrap();
        let mut form = OrderForm::opened(bot.clone(), IdempotencyKey::new("k").unwrap());
        assert!(form.is_open_for(&bot) && !form.is_open_for(&BotId::new("other").unwrap()));
        assert!(!form.ready());
        form.text = "  ".into();
        assert!(!form.ready());
        form.text = "Roll back service-1".into();
        assert!(form.ready());
        assert!(!OrderForm::default().is_open_for(&bot));
    }
}
