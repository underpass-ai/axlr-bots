use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// One thing a running task's execution did, as its node read it from
/// the run: a `bot.task.progress` event holds a batch of them. The kind
/// says which fields it has: `tool.started` (tool, the MCP tool a bridged
/// call names, a short summary of its arguments), `tool.ended` (status
/// and how long it took), `decision.required` (the tool waiting for a
/// person), `delegation` (the bot asked, the child task or why it was
/// refused), `text` (the model's words) and `text.truncated`.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct ProgressItem {
    pub kind: String,
    pub at: Option<DateTime<Utc>>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub tool: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub mcp_tool: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub summary: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub duration_ms: Option<u64>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub bot: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub task_id: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub reason: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub text: String,
}
