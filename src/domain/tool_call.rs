use serde::{Deserialize, Serialize};

/// A tool call of an execution: the tool, the policy's decision and how
/// the call ended.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ToolCall {
    pub name: String,
    #[serde(default)]
    pub call_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub decision: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub status: String,
}

/// How a decision reads at a glance.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CallVerdict {
    /// Let through: by a person or by the policy.
    Allowed,
    /// Refused: by a person or by the policy.
    Refused,
    /// Stopped for a person.
    Asking,
    Unknown,
}

impl ToolCall {
    pub fn verdict(&self) -> CallVerdict {
        match self.decision.as_str() {
            "approve" | "auto_approve" | "allow" | "allowed" => CallVerdict::Allowed,
            "deny" | "auto_deny" | "denied" | "refuse" | "forbidden" => CallVerdict::Refused,
            "ask" | "approval" | "pending" => CallVerdict::Asking,
            _ => CallVerdict::Unknown,
        }
    }

    /// The decision in short words: `person OK`, `auto OK`, `refused`.
    pub fn decision_label(&self) -> &'static str {
        match self.decision.as_str() {
            "approve" => "person OK",
            "auto_approve" | "allow" | "allowed" => "auto OK",
            "deny" => "person no",
            "auto_deny" | "denied" | "refuse" | "forbidden" => "refused",
            "ask" | "approval" | "pending" => "asking",
            "" => "—",
            _ => "other",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn call(decision: &str) -> ToolCall {
        ToolCall {
            name: "local_write".into(),
            decision: decision.into(),
            ..Default::default()
        }
    }

    #[test]
    fn reads_the_decision() {
        assert_eq!(call("approve").verdict(), CallVerdict::Allowed);
        assert_eq!(call("approve").decision_label(), "person OK");
        assert_eq!(call("auto_approve").decision_label(), "auto OK");
        assert_eq!(call("deny").verdict(), CallVerdict::Refused);
        assert_eq!(call("deny").decision_label(), "person no");
        assert_eq!(call("auto_deny").decision_label(), "refused");
        assert_eq!(call("ask").verdict(), CallVerdict::Asking);
        assert_eq!(call("ask").decision_label(), "asking");
        assert_eq!(call("").decision_label(), "—");
        assert_eq!(call("maybe").verdict(), CallVerdict::Unknown);
        assert_eq!(call("maybe").decision_label(), "other");
    }
}
