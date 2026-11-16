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

    /// Why the bot made the call, in its own words: the `why` its
    /// arguments carry, as many bots' tools ask for one. The summary is
    /// often cut short, so a `why` whose string the cut left open is read
    /// up to the cut. None when the arguments have no `why`.
    pub fn why(&self) -> Option<String> {
        let read = match serde_json::from_str::<serde_json::Value>(&self.summary) {
            Ok(serde_json::Value::Object(fields)) => fields.get("why").and_then(|v| v.as_str()).map(str::to_string),
            Ok(_) => None,
            Err(_) => leading_why(&self.summary),
        };
        read.map(|why| why.trim().to_string()).filter(|why| !why.is_empty())
    }
}

/// The string of a `"why"` field in arguments that are not whole JSON:
/// from the field's opening quote to its closing one, or to the end of
/// what is there when the cut came first; its escapes read.
fn leading_why(summary: &str) -> Option<String> {
    let at = summary.find("\"why\"")?;
    let after = summary[at + 5..].trim_start().strip_prefix(':')?.trim_start();
    let mut chars = after.strip_prefix('"')?.chars();
    let mut why = String::new();
    while let Some(c) = chars.next() {
        match c {
            '"' => break,
            '\\' => match chars.next() {
                Some('n') | Some('t') | Some('r') => why.push(' '),
                Some('u') => {
                    let code: String = chars.by_ref().take(4).collect();
                    if let Some(c) = u32::from_str_radix(&code, 16).ok().and_then(char::from_u32) {
                        why.push(c);
                    }
                }
                Some(other) => why.push(other),
                None => break,
            },
            other => why.push(other),
        }
    }
    Some(why)
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

    fn with_summary(summary: &str) -> ToolActivity {
        ToolActivity {
            call_id: "c1".into(),
            tool: "local_read".into(),
            mcp_tool: String::new(),
            summary: summary.into(),
            state: ToolState::Ok,
            error: String::new(),
            started_at: "2026-11-02T10:00:00Z".parse().unwrap(),
            duration_ms: Some(6),
        }
    }

    #[test]
    fn reads_why_the_call_was_made_from_its_arguments() {
        let whole = with_summary(r#"{"why":"Read the notes before the round.","path":"notes.md"}"#);
        assert_eq!(whole.why().as_deref(), Some("Read the notes before the round."));
        let later = with_summary(r#"{"path":"notes.md","why":" Then the log. "}"#);
        assert_eq!(later.why().as_deref(), Some("Then the log."));
        // Cut short by the node: read up to the cut, escapes and all.
        let cut = with_summary(r#"{"why":"Say \"done\" when the file\nis écrit and the rest is cut"#);
        assert_eq!(
            cut.why().as_deref(),
            Some(r#"Say "done" when the file is écrit and the rest is cut"#)
        );
        let closed = with_summary(r#"{"why" : "Check the disk", "path":"/var/lo"#);
        assert_eq!(closed.why().as_deref(), Some("Check the disk"));
        for none in [
            r#"{"path":"notes.md"}"#,
            r#"{"why":""}"#,
            r#"{"why":42}"#,
            "[1,2]",
            "",
            "plain words",
            r#"{"why""#,
        ] {
            assert_eq!(with_summary(none).why(), None, "{none}");
        }
    }
}
