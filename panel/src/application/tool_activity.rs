use chrono::{DateTime, Utc};

use super::ToolState;

/// One tool call of a run as it went: what it called, a short summary of
/// its arguments when the run's lines carry them, and how it ended.
#[derive(Debug, Clone, PartialEq)]
pub struct ToolActivity {
    pub call_id: String,
    pub tool: String,
    /// The MCP tool a bridged call (`axlr_call_tool`) names.
    pub mcp_tool: String,
    pub summary: String,
    pub state: ToolState,
    pub started_at: DateTime<Utc>,
    pub duration_ms: Option<u64>,
}

impl ToolActivity {
    /// The name a person knows the call by: the MCP tool it bridges to,
    /// or its own.
    pub fn name(&self) -> String {
        if self.mcp_tool.is_empty() {
            self.tool.clone()
        } else {
            format!("{} · {}", self.mcp_tool, self.tool)
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
            started_at: "2026-11-02T10:00:00Z".parse().unwrap(),
            duration_ms: Some(2400),
        };
        assert_eq!(
            (call.name().as_str(), call.took().as_str()),
            ("list_pods · axlr_call_tool", "2.4 s")
        );
        call.mcp_tool.clear();
        call.duration_ms = Some(120);
        assert_eq!(
            (call.name().as_str(), call.took().as_str()),
            ("axlr_call_tool", "120 ms")
        );
        call.duration_ms = None;
        assert_eq!(call.took(), "");
    }
}
