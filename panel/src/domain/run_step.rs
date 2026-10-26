use chrono::{DateTime, Utc};
use serde::Deserialize;

use super::{CallVerdict, ToolCall};

/// One step of a run: a tool call, when it started, how long it took,
/// what decided it and how it ended. Read from the run's receipt when it
/// has one, else from the result's list of calls.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct RunStep {
    #[serde(default, rename = "call_id")]
    pub call_id: String,
    pub tool: String,
    #[serde(default)]
    pub decision: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub started_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub duration_ms: Option<i64>,
}

impl RunStep {
    /// The steps of a receipt (`axlr.execution-receipt/v1`), in order;
    /// none when it is not one.
    pub fn from_receipt(receipt: &serde_json::Value) -> Vec<Self> {
        let Some(calls) = receipt.get("tool_calls").and_then(serde_json::Value::as_array) else {
            return Vec::new();
        };
        let mut steps: Vec<Self> = calls
            .iter()
            .filter_map(|call| serde_json::from_value(call.clone()).ok())
            .collect();
        steps.sort_by_key(|s| s.started_at);
        steps
    }

    /// A step from the result's list of calls, without timing.
    pub fn from_call(call: &ToolCall) -> Self {
        Self {
            call_id: call.call_id.clone(),
            tool: call.name.clone(),
            decision: call.decision.clone(),
            status: call.status.clone(),
            started_at: None,
            duration_ms: None,
        }
    }

    fn call(&self) -> ToolCall {
        ToolCall {
            name: self.tool.clone(),
            call_id: self.call_id.clone(),
            decision: self.decision.clone(),
            status: self.status.clone(),
        }
    }

    pub fn verdict(&self) -> CallVerdict {
        self.call().verdict()
    }

    /// Why the call ran or not, in a person's words.
    pub fn why(&self) -> &'static str {
        if self.decision.is_empty() && self.status == "pending" {
            return "It asked you";
        }
        match self.decision.as_str() {
            "auto_approve" | "allow" | "allowed" => "Allowed by its rules",
            "approve" => "You approved it",
            "deny" => "You denied it",
            "auto_deny" | "denied" | "refuse" | "forbidden" => "Blocked by its rules",
            "ask" | "approval" | "pending" => "It asked you",
            "" => "No decision needed",
            _ => "Decided another way",
        }
    }

    /// How the call ended, in a word or two.
    pub fn ended(&self) -> &str {
        match self.status.as_str() {
            "completed" | "ok" | "succeeded" => "done",
            "failed" | "error" => "failed",
            "pending" | "running" => "waiting",
            "denied" | "refused" | "skipped" => "not run",
            "" => "—",
            other => other,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_steps_of_a_receipt_in_order() {
        let receipt = serde_json::json!({
            "schema": "axlr.execution-receipt/v1",
            "tool_calls": [
                {"call_id": "c2", "tool": "local_write", "decision": "approve", "status": "completed",
                 "started_at": "2026-10-26T20:25:16Z", "duration_ms": 12, "arguments_sha256": "ab"},
                {"call_id": "c1", "tool": "local_read", "decision": "auto_approve", "status": "completed",
                 "started_at": "2026-10-26T20:25:15Z", "duration_ms": 2},
                {"no": "tool"}
            ]
        });
        let steps = RunStep::from_receipt(&receipt);
        assert_eq!(
            steps.iter().map(|s| s.tool.as_str()).collect::<Vec<_>>(),
            vec!["local_read", "local_write"]
        );
        assert_eq!(steps[0].why(), "Allowed by its rules");
        assert_eq!(steps[1].why(), "You approved it");
        assert_eq!((steps[1].duration_ms, steps[1].ended()), (Some(12), "done"));
        assert_eq!(steps[0].verdict(), CallVerdict::Allowed);
        assert!(RunStep::from_receipt(&serde_json::json!([1, 2])).is_empty());
    }

    #[test]
    fn says_why_and_how_in_words() {
        let step = |decision: &str, status: &str| RunStep {
            call_id: String::new(),
            tool: "t".into(),
            decision: decision.into(),
            status: status.into(),
            started_at: None,
            duration_ms: None,
        };
        assert_eq!(step("", "pending").why(), "It asked you");
        assert_eq!(step("ask", "pending").why(), "It asked you");
        assert_eq!(step("deny", "denied").why(), "You denied it");
        assert_eq!(step("auto_deny", "denied").why(), "Blocked by its rules");
        assert_eq!(step("", "completed").why(), "No decision needed");
        assert_eq!(step("other", "x").why(), "Decided another way");
        assert_eq!(step("", "failed").ended(), "failed");
        assert_eq!(step("", "running").ended(), "waiting");
        assert_eq!(step("", "denied").ended(), "not run");
        assert_eq!(step("", "").ended(), "—");
        assert_eq!(step("", "odd").ended(), "odd");
        let call = ToolCall {
            name: "local_read".into(),
            call_id: "c1".into(),
            decision: "auto_approve".into(),
            status: "completed".into(),
        };
        let from = RunStep::from_call(&call);
        assert_eq!((from.tool.as_str(), from.started_at), ("local_read", None));
    }
}
