/// Where one tool call of a run stands, as its progress says.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolState {
    /// Asked for; no end seen yet.
    Running,
    /// It waits for a person's OK.
    Waiting,
    Ok,
    /// A person or the bot's rules said no.
    Denied,
    Failed,
    /// The task ended and no end of the call was seen.
    Unfinished,
}

impl ToolState {
    /// How the node says a call ended: `ok`, `denied` or `failed`.
    pub fn ended(status: &str) -> Self {
        match status {
            "denied" => Self::Denied,
            "failed" => Self::Failed,
            _ => Self::Ok,
        }
    }

    /// The state in a word or two.
    pub fn label(self) -> &'static str {
        match self {
            Self::Running => "running",
            Self::Waiting => "needs OK",
            Self::Ok => "ok",
            Self::Denied => "denied",
            Self::Failed => "failed",
            Self::Unfinished => "no end seen",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_how_a_call_ended() {
        assert_eq!(ToolState::ended("ok"), ToolState::Ok);
        assert_eq!(ToolState::ended("denied"), ToolState::Denied);
        assert_eq!(ToolState::ended("failed"), ToolState::Failed);
        assert_eq!(ToolState::ended("something new"), ToolState::Ok);
        let labels: Vec<&str> = [
            ToolState::Running,
            ToolState::Waiting,
            ToolState::Ok,
            ToolState::Denied,
            ToolState::Failed,
            ToolState::Unfinished,
        ]
        .iter()
        .map(|s| s.label())
        .collect();
        assert_eq!(labels, ["running", "needs OK", "ok", "denied", "failed", "no end seen"]);
    }
}
