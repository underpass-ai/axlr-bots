use chrono::{DateTime, Utc};

use super::ToolState;

/// One tool call of a run as it went: what it called, a short summary of
/// its arguments when the run's lines carry them, and how it ended, with
/// the first line of the error when it failed.
#[derive(Debug, Clone, PartialEq)]
pub struct ToolActivity {
    pub call_id: String,
    pub tool: String,
    /// The MCP tool a bridged call (`axlr_call_tool`) names.
    pub mcp_tool: String,
    pub summary: String,
    pub state: ToolState,
    pub error: String,
    pub started_at: DateTime<Utc>,
    pub duration_ms: Option<u64>,
}

impl ToolActivity {
    /// The name a person knows the call by: the MCP tool it bridges to,
    /// or its own.
    pub fn name(&self) -> &str {
        if self.mcp_tool.is_empty() {
            &self.tool
        } else {
            &self.mcp_tool
        }
    }

    /// The arguments' summary in at most `limit` characters, for one line.
    pub fn short_summary(&self, limit: usize) -> String {
        let mut chars = self.summary.chars();
        let head: String = chars.by_ref().take(limit).collect();
        if chars.next().is_some() {
            format!("{}…", head.chars().take(limit.saturating_sub(1)).collect::<String>())
        } else {
            head
        }
    }

    /// How long it took, `120 ms` or `2.4 s`; empty until it ended.
    pub fn took(&self) -> String {
        match self.duration_ms {
            Some(ms) if ms < 1000 => format!("{ms} ms"),
            Some(ms) => format!("{:.1} s", ms as f64 / 1000.0),
            None => String::new(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_the_bridged_tool_and_says_how_long() {
        let mut call = ToolActivity {
            call_id: "c1".into(),
            tool: "axlr_call_tool".into(),
            mcp_tool: "list_pods".into(),
            summary: String::new(),
            state: ToolState::Ok,
            error: String::new(),
            started_at: "2026-11-02T10:00:00Z".parse().unwrap(),
            duration_ms: Some(2400),
        };
        assert_eq!((call.name(), call.took().as_str()), ("list_pods", "2.4 s"));
        call.mcp_tool.clear();
        call.duration_ms = Some(120);
        assert_eq!((call.name(), call.took().as_str()), ("axlr_call_tool", "120 ms"));
        call.duration_ms = None;
        assert_eq!(call.took(), "");
    }

    #[test]
    fn a_long_summary_is_cut_to_one_line() {
        let mut call = ToolActivity {
            call_id: "c1".into(),
            tool: "axlr_call_tool".into(),
            mcp_tool: "get_status".into(),
            summary: r#"{"env":"pre"}"#.into(),
            state: ToolState::Running,
            error: String::new(),
            started_at: "2026-11-02T10:00:00Z".parse().unwrap(),
            duration_ms: None,
        };
        assert_eq!(call.short_summary(20), r#"{"env":"pre"}"#);
        call.summary = "é".repeat(30);
        let cut = call.short_summary(20);
        assert_eq!((cut.chars().count(), cut.ends_with('…')), (20, true));
    }
}
