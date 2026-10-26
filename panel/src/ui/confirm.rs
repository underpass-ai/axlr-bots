use crate::domain::{BotId, NodeId, TaskId};

/// An act that cannot be undone, waiting for a second click.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Confirm {
    Revoke(NodeId),
    Cancel(TaskId),
    Deny(TaskId),
    StopBot(BotId),
}

impl Confirm {
    pub fn title(&self) -> String {
        match self {
            Self::Revoke(id) => format!("Revoke {id}?"),
            Self::Cancel(id) => format!("Stop {id}?"),
            Self::Deny(_) => "Deny this call?".to_string(),
            Self::StopBot(id) => format!("Stop {id}?"),
        }
    }

    /// What happens, in one or two short lines.
    pub fn lines(&self) -> [&'static str; 2] {
        match self {
            Self::Revoke(_) => [
                "Its certificate stops working now.",
                "Its running tasks fail. A new token brings it back.",
            ],
            Self::Cancel(_) => ["The task stops and does not run again.", "A Pending task never starts."],
            Self::Deny(_) => ["The bot does not make this call.", "The run goes on without it."],
            Self::StopBot(_) => [
                "It is paused: it takes no new task.",
                "Its pending, running and waiting tasks are cancelled.",
            ],
        }
    }

    /// The words on the button that does it.
    pub fn act(&self) -> &'static str {
        match self {
            Self::Revoke(_) => "Revoke",
            Self::Cancel(_) => "Stop task",
            Self::Deny(_) => "Deny",
            Self::StopBot(_) => "Stop bot",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_what_will_happen() {
        let revoke = Confirm::Revoke(NodeId::new("wrx80").unwrap());
        assert_eq!(revoke.title(), "Revoke wrx80?");
        assert_eq!(revoke.act(), "Revoke");
        let cancel = Confirm::Cancel(TaskId::new("task-1").unwrap());
        assert_eq!(cancel.title(), "Stop task-1?");
        assert!(cancel.lines()[0].contains("stops"));
        let deny = Confirm::Deny(TaskId::new("task-1").unwrap());
        assert_eq!((deny.title().as_str(), deny.act()), ("Deny this call?", "Deny"));
        let stop = Confirm::StopBot(BotId::new("pilot").unwrap());
        assert_eq!((stop.title().as_str(), stop.act()), ("Stop pilot?", "Stop bot"));
        assert!(stop.lines()[1].contains("cancelled"));
    }
}
