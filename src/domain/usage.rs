use serde::{Deserialize, Serialize};

/// What an execution used: model requests, tokens, tool calls and cost.
#[derive(Debug, Clone, Copy, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Usage {
    #[serde(default)]
    pub requests: u32,
    #[serde(default)]
    pub prompt_tokens: u64,
    #[serde(default)]
    pub cached_tokens: u64,
    #[serde(default)]
    pub completion_tokens: u64,
    #[serde(default)]
    pub reasoning_tokens: u64,
    #[serde(default)]
    pub tool_calls: u32,
    #[serde(default)]
    pub cost_usd: f64,
}

impl Usage {
    /// Prompt plus completion tokens.
    pub fn total_tokens(&self) -> u64 {
        self.prompt_tokens + self.completion_tokens
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn adds_the_tokens() {
        let usage: Usage = serde_json::from_str(
            r#"{"requests":5,"promptTokens":37774,"completionTokens":565,"costUsd":0.0016}"#,
        )
        .unwrap();
        assert_eq!(usage.total_tokens(), 38339);
        assert_eq!(usage.requests, 5);
        assert_eq!(usage.cached_tokens, 0);
    }
}
