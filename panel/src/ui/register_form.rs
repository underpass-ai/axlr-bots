use crate::domain::BotForm;

/// The form that registers a bot.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RegisterForm {
    pub open: bool,
    pub form: BotForm,
    pub busy: bool,
}

impl RegisterForm {
    /// A fresh form with the tools a reading bot needs.
    pub fn opened() -> Self {
        Self {
            open: true,
            form: BotForm {
                tools: "local_read, local_list, local_search".into(),
                model: "anthropic/claude-haiku-5.5".into(),
                ..BotForm::default()
            },
            busy: false,
        }
    }
}
