use serde::{Deserialize, Serialize};

use super::Cost;

/// What an execution used: model requests, tokens, tool calls and cost.
/// A run stopped before it said what it spent has its cost unknown, and
/// `cost_usd` at 0.
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
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub cost_unknown: bool,
}

impl Usage {
    /// Prompt plus completion tokens.
    pub fn total_tokens(&self) -> u64 {
        self.prompt_tokens + self.completion_tokens
    }

    /// What the run cost, as far as it is known.
    pub fn cost(&self) -> Cost {
        Cost::new(self.cost_usd, u32::from(self.cost_unknown))
    }

    /// Two usages added up; the cost stays unknown when either is.
    pub fn plus(self, other: Usage) -> Usage {
        Usage {
            requests: self.requests + other.requests,
            prompt_tokens: self.prompt_tokens + other.prompt_tokens,
            cached_tokens: self.cached_tokens + other.cached_tokens,
            completion_tokens: self.completion_tokens + other.completion_tokens,
            reasoning_tokens: self.reasoning_tokens + other.reasoning_tokens,
            tool_calls: self.tool_calls + other.tool_calls,
            cost_usd: self.cost_usd + other.cost_usd,
            cost_unknown: self.cost_unknown || other.cost_unknown,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn adds_the_tokens() {
        let usage: Usage =
            serde_json::from_str(r#"{"requests":5,"promptTokens":37774,"completionTokens":565,"costUsd":0.0016}"#)
                .unwrap();
        assert_eq!(usage.total_tokens(), 38339);
        assert_eq!(usage.requests, 5);
        assert_eq!(usage.cached_tokens, 0);
        assert_eq!(usage.cost(), Cost::new(0.0016, 0));
        let unknown: Usage = serde_json::from_str(r#"{"requests":0,"costUsd":0,"costUnknown":true}"#).unwrap();
        assert_eq!(unknown.cost(), Cost::new(0.0, 1));
        assert!(!serde_json::to_string(&usage).unwrap().contains("costUnknown"));
    }
}
