use std::fmt;

use crate::domain::{BotId, DeliveryMode};

/// The form that makes a bot listen to a topic: the topic, the mode (by
/// the kind of what is typed when none is picked) and whom from.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ListenForm {
    pub bot: Option<BotId>,
    pub topic: String,
    pub mode: ModeChoice,
    pub from: String,
    pub busy: bool,
}

impl ListenForm {
    pub fn opened(bot: BotId) -> Self {
        Self {
            bot: Some(bot),
            ..Self::default()
        }
    }

    /// Whether it is open for the bot.
    pub fn is_open_for(&self, bot: &BotId) -> bool {
        self.bot.as_ref() == Some(bot)
    }

    /// The mode the subscription gets: the one picked, else none (the
    /// kind's, which the form shows).
    pub fn mode(&self) -> Option<DeliveryMode> {
        match self.mode {
            ModeChoice::ByKind => None,
            ModeChoice::Mode(mode) => Some(mode),
        }
    }

    /// What the form says the default is, for what is typed now:
    /// `by kind: message`.
    pub fn default_label(&self) -> String {
        format!("by kind: {}", DeliveryMode::by_kind(self.topic.trim()))
    }
}

/// An entry of the mode picker: by the kind, or one mode.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum ModeChoice {
    #[default]
    ByKind,
    Mode(DeliveryMode),
}

impl ModeChoice {
    pub const ALL: [ModeChoice; 4] = [
        ModeChoice::ByKind,
        ModeChoice::Mode(DeliveryMode::Task),
        ModeChoice::Mode(DeliveryMode::Message),
        ModeChoice::Mode(DeliveryMode::Note),
    ];
}

impl fmt::Display for ModeChoice {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::ByKind => f.write_str("by its kind"),
            Self::Mode(DeliveryMode::Task) => f.write_str("task: a new task"),
            Self::Mode(DeliveryMode::Message) => f.write_str("message: into its running task"),
            Self::Mode(DeliveryMode::Note) => f.write_str("note: for its next task"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shows_the_default_by_the_kind() {
        let mut form = ListenForm::opened(BotId::new("bot").unwrap());
        assert!(form.is_open_for(&BotId::new("bot").unwrap()));
        form.topic = "warn.stock".into();
        assert_eq!(form.default_label(), "by kind: message");
        assert_eq!(form.mode(), None);
        form.mode = ModeChoice::Mode(DeliveryMode::Note);
        assert_eq!(form.mode(), Some(DeliveryMode::Note));
        let labels: Vec<String> = ModeChoice::ALL.iter().map(|m| m.to_string()).collect();
        assert_eq!(labels[0], "by its kind");
        assert!(labels[2].starts_with("message"));
    }
}
