use chrono::{DateTime, Utc};

use super::{DelegationActivity, ToolActivity, ToolState};

/// One line of a task's Activity: a tool call, a bot asked, a call that
/// waits for a person, what the person wrote to the run and what it said
/// back.
#[derive(Debug, Clone, PartialEq)]
pub enum ActivityEntry {
    Tool(ToolActivity),
    Delegation(DelegationActivity),
    Waiting { tool: String, at: DateTime<Utc> },
    YouWrote { text: String, at: DateTime<Utc> },
    Said { text: String, at: DateTime<Utc> },
}

impl ActivityEntry {
    /// When it happened: a call when it started.
    pub fn at(&self) -> DateTime<Utc> {
        match self {
            Self::Tool(call) => call.started_at,
            Self::Delegation(asked) => asked.at,
            Self::Waiting { at, .. } | Self::YouWrote { at, .. } | Self::Said { at, .. } => *at,
        }
    }

    /// The entry in one line, without its time.
    pub fn headline(&self) -> String {
        match self {
            Self::Tool(call) => {
                let mut line = format!("{} · {}", call.name(), call.state.label());
                if !call.took().is_empty() {
                    line = format!("{line} · {}", call.took());
                }
                line
            }
            Self::Delegation(asked) => match (&asked.task, asked.refused.is_empty()) {
                (_, false) => format!("Asked {} · refused: {}", asked.bot, asked.refused),
                (Some(task), true) => format!("Asked {} · task {task}", asked.bot),
                (None, true) => format!("Asked {}", asked.bot),
            },
            Self::Waiting { tool, .. } => format!("Waits for your OK to call {tool}"),
            Self::YouWrote { text, .. } => format!("You wrote: {text}"),
            Self::Said { text, .. } => format!("It answered: {text}"),
        }
    }

    /// Everything the entry knows, label and value, for its details.
    pub fn details(&self) -> Vec<(&'static str, String)> {
        let mut out = Vec::new();
        let mut put = |label: &'static str, value: String| {
            if !value.is_empty() {
                out.push((label, value));
            }
        };
        match self {
            Self::Tool(call) => {
                put("Tool", call.tool.clone());
                put("MCP tool", call.mcp_tool.clone());
                put("State", call.state.label().to_string());
                put("Took", call.took());
                put("Arguments", call.summary.clone());
                put("Call id", call.call_id.clone());
            }
            Self::Delegation(asked) => {
                put("Bot", asked.bot.clone());
                put("Task", asked.task.as_ref().map(|t| t.to_string()).unwrap_or_default());
                put("Refused", asked.refused.clone());
            }
            Self::Waiting { tool, .. } => put("Tool", tool.clone()),
            Self::YouWrote { text, .. } | Self::Said { text, .. } => put("Words", text.clone()),
        }
        out
    }

    /// The details as text to copy, one per line.
    pub fn details_text(&self) -> String {
        let mut lines = vec![self.headline()];
        lines.extend(
            self.details()
                .into_iter()
                .map(|(label, value)| format!("{label}: {value}")),
        );
        lines.join("\n")
    }

    /// Whether it is a call still going.
    pub fn is_running(&self) -> bool {
        matches!(self, Self::Tool(call) if call.state == ToolState::Running)
    }
}
