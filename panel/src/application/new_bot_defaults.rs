/// What the New bot form starts with: the model it proposes and the
/// models it offers as one-click choices. A panel preference about the
/// form, not a bot's own configuration: the bot keeps its model in its
/// definition.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NewBotDefaults {
    pub model: String,
    pub models: Vec<String>,
}

impl Default for NewBotDefaults {
    fn default() -> Self {
        Self {
            model: "anthropic/claude-haiku-5.5".into(),
            models: vec!["anthropic/claude-haiku-5.5".into()],
        }
    }
}

impl NewBotDefaults {
    /// The models offered, the proposed one first, each once, in order.
    pub fn choices(&self) -> Vec<String> {
        let mut choices = Vec::new();
        for model in std::iter::once(&self.model).chain(self.models.iter()) {
            let model = model.trim();
            if !model.is_empty() && !choices.iter().any(|c| c == model) {
                choices.push(model.to_string());
            }
        }
        choices
    }

    /// Trimmed, with the empty names dropped.
    pub fn tidy(&self) -> Self {
        Self {
            model: self.model.trim().to_string(),
            models: self
                .models
                .iter()
                .map(|m| m.trim().to_string())
                .filter(|m| !m.is_empty())
                .collect(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn offers_each_model_once_with_the_proposed_one_first() {
        let defaults = NewBotDefaults {
            model: " codex/gpt-6-luna ".into(),
            models: vec![
                "anthropic/claude-haiku-5.5".into(),
                "codex/gpt-6-luna".into(),
                " ".into(),
            ],
        };
        assert_eq!(
            defaults.choices(),
            vec!["codex/gpt-6-luna".to_string(), "anthropic/claude-haiku-5.5".into()]
        );
        let tidy = defaults.tidy();
        assert_eq!(tidy.model, "codex/gpt-6-luna");
        assert_eq!(tidy.models.len(), 2);
        assert_eq!(NewBotDefaults::default().choices().len(), 1);
    }
}
