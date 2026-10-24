use serde::{Deserialize, Serialize};

/// Which model a bot uses and how far its requests may travel.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelPolicy {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub model: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub routing_policy: String,
    #[serde(default)]
    pub allow_remote_providers: bool,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub max_context_tokens: u32,
}

fn is_zero(value: &u32) -> bool {
    *value == 0
}

impl ModelPolicy {
    /// The model's name without its provider, for a narrow cell.
    pub fn short_model(&self) -> &str {
        if self.model.is_empty() {
            return "default";
        }
        self.model.rsplit('/').next().unwrap_or(&self.model)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shortens_the_model_name() {
        let policy = ModelPolicy {
            model: "anthropic/claude-haiku-5.5".into(),
            ..Default::default()
        };
        assert_eq!(policy.short_model(), "claude-haiku-5.5");
        assert_eq!(ModelPolicy::default().short_model(), "default");
        assert_eq!(
            serde_json::to_string(&ModelPolicy::default()).unwrap(),
            r#"{"allowRemoteProviders":false}"#
        );
    }
}
