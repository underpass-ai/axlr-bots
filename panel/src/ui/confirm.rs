use super::table::BulkPlan;
use crate::domain::{BotId, NodeId, TaskId};

/// An act that cannot be undone, or one on many rows, waiting for a
/// second click.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Confirm {
    Revoke(NodeId),
    Cancel(TaskId),
    Deny(TaskId),
    StopBot(BotId),
    /// One act on the rows picked in a list.
    Bulk(BulkPlan),
}

impl Confirm {
    pub fn title(&self) -> String {
        match self {
            Self::Revoke(id) => format!("Revoke {id}?"),
            Self::Cancel(id) => format!("Stop {id}?"),
            Self::Deny(_) => "Deny this call?".to_string(),
            Self::StopBot(id) => format!("Stop {id}?"),
            Self::Bulk(plan) => plan.title(),
        }
    }

    /// What happens, in a few short lines.
    pub fn lines(&self) -> Vec<String> {
        let lines: [&str; 2] = match self {
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
            Self::Bulk(plan) => return plan.lines(),
        };
        lines.iter().map(|line| line.to_string()).collect()
    }

    /// The words on the button that does it.
    pub fn act(&self) -> String {
        match self {
            Self::Revoke(_) => "Revoke".into(),
            Self::Cancel(_) => "Stop task".into(),
            Self::Deny(_) => "Deny".into(),
            Self::StopBot(_) => "Stop".into(),
            Self::Bulk(plan) => plan.act(),
        }
    }

    /// It ends work or cannot be undone: its button is red. A bulk
    /// Pause, Turn on, Approve or Resolve is not.
    pub fn destructive(&self) -> bool {
        match self {
            Self::Bulk(plan) => plan.action.destructive(),
            _ => true,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::{BulkAction, BulkTarget};
    use crate::ui::table::List;

    #[test]
    fn says_what_will_happen() {
        let revoke = Confirm::Revoke(NodeId::new("wrx80").unwrap());
        assert_eq!(revoke.title(), "Revoke wrx80?");
        assert_eq!(revoke.act(), "Revoke");
        let cancel = Confirm::Cancel(TaskId::new("task-1").unwrap());
        assert_eq!(cancel.title(), "Stop task-1?");
        assert!(cancel.lines()[0].contains("stops"));
        let deny = Confirm::Deny(TaskId::new("task-1").unwrap());
        assert_eq!(
            (deny.title().as_str(), deny.act().as_str()),
            ("Deny this call?", "Deny")
        );
        let stop = Confirm::StopBot(BotId::new("worker-0").unwrap());
        assert_eq!((stop.title().as_str(), stop.act().as_str()), ("Stop worker-0?", "Stop"));
        assert!(stop.lines()[1].contains("cancelled"));
        assert!(stop.destructive());
    }

    #[test]
    fn a_bulk_act_names_its_rows_and_looks_as_harsh_as_it_is() {
        let plan = |action| BulkPlan {
            action,
            unit: List::Bots,
            targets: vec![BulkTarget::Bot(BotId::new("worker-1").unwrap())],
            weighed: 2,
            teams: Vec::new(),
        };
        let pause = Confirm::Bulk(plan(BulkAction::Pause));
        assert_eq!(
            (pause.title().as_str(), pause.act().as_str()),
            ("Pause 1 bot?", "Pause 1")
        );
        assert_eq!(pause.lines()[0], "worker-1.");
        assert!(!pause.destructive());
        assert!(Confirm::Bulk(plan(BulkAction::Stop)).destructive());
    }
}
