use crate::domain::{AutonomyLevel, BotForm};

/// The form that registers a bot.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RegisterForm {
    pub open: bool,
    pub form: BotForm,
    pub busy: bool,
    /// The level, tools and model are shown.
    pub advanced: bool,
}

impl RegisterForm {
    /// A fresh form with sensible defaults: the tools a reading bot needs,
    /// and it asks before acting.
    pub fn opened() -> Self {
        Self {
            open: true,
            form: BotForm {
                level: AutonomyLevel::ActWithApproval,
                tools: "local_read, local_list, local_search".into(),
                model: "anthropic/claude-haiku-5.5".into(),
                ..BotForm::default()
            },
            busy: false,
            advanced: false,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_new_bot_asks_before_acting_with_advanced_closed() {
        let form = RegisterForm::opened();
        assert!(form.open && !form.advanced && !form.busy);
        assert_eq!(form.form.level, AutonomyLevel::ActWithApproval);
        assert!(form.form.tools.contains("local_read"));
        assert!(!RegisterForm::default().open);
    }
}
