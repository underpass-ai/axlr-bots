/// What a person does to many rows of a list at once, from the bar over
/// the list: each is one call per row, the same call its row's button
/// makes.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum BulkAction {
    /// Switch bots on.
    TurnOn,
    Pause,
    /// Stop bots: pause them and cancel their open tasks.
    Stop,
    /// Delete bots for good.
    Delete,
    /// Answer the calls tasks wait on.
    Approve,
    Deny,
    /// Stop tasks.
    Cancel,
    /// Revoke nodes' certificates.
    Revoke,
    /// Close incidents by hand.
    Resolve,
}

impl BulkAction {
    /// The words on its button.
    pub fn label(self) -> &'static str {
        match self {
            Self::TurnOn => "Turn on",
            Self::Pause => "Pause",
            Self::Stop | Self::Cancel => "Stop",
            Self::Delete => "Delete",
            Self::Approve => "Approve",
            Self::Deny => "Deny",
            Self::Revoke => "Revoke",
            Self::Resolve => "Resolve",
        }
    }

    /// What became of a row it went through for: `4 paused`.
    pub fn past(self) -> &'static str {
        match self {
            Self::TurnOn => "turned on",
            Self::Pause => "paused",
            Self::Stop | Self::Cancel => "stopped",
            Self::Delete => "deleted",
            Self::Approve => "approved",
            Self::Deny => "denied",
            Self::Revoke => "revoked",
            Self::Resolve => "resolved",
        }
    }

    /// It cannot be taken back, or it ends work: its buttons look so.
    pub fn destructive(self) -> bool {
        matches!(
            self,
            Self::Stop | Self::Delete | Self::Deny | Self::Cancel | Self::Revoke
        )
    }

    /// What it does to each row, in a line for its dialog.
    pub fn what_happens(self) -> &'static str {
        match self {
            Self::TurnOn => "Each takes tasks again.",
            Self::Pause => "Each takes no new task; what runs goes on.",
            Self::Stop => "Each is paused and its open tasks are cancelled.",
            Self::Delete => {
                "Each is gone for good: its open tasks are cancelled, its timers removed; bots that name it no longer reach it."
            }
            Self::Approve => "Each bot makes the call it waits on.",
            Self::Deny => "No bot makes the call it waits on; each run goes on without it.",
            Self::Cancel => "Each task stops and does not run again.",
            Self::Revoke => "Each certificate stops working now; their running tasks fail.",
            Self::Resolve => "Each incident closes; a new alert opens a new one.",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_the_act_and_what_became_of_it() {
        assert_eq!(
            (BulkAction::Pause.label(), BulkAction::Pause.past()),
            ("Pause", "paused")
        );
        assert_eq!(BulkAction::TurnOn.past(), "turned on");
        assert_eq!(
            (BulkAction::Cancel.label(), BulkAction::Cancel.past()),
            ("Stop", "stopped")
        );
        assert_eq!(BulkAction::Resolve.past(), "resolved");
        assert_eq!(
            (BulkAction::Delete.label(), BulkAction::Delete.past()),
            ("Delete", "deleted")
        );
    }

    #[test]
    fn only_acts_that_end_work_look_destructive() {
        for action in [
            BulkAction::Stop,
            BulkAction::Delete,
            BulkAction::Deny,
            BulkAction::Cancel,
            BulkAction::Revoke,
        ] {
            assert!(action.destructive(), "{action:?}");
        }
        for action in [
            BulkAction::TurnOn,
            BulkAction::Pause,
            BulkAction::Approve,
            BulkAction::Resolve,
        ] {
            assert!(!action.destructive(), "{action:?}");
            assert!(action.what_happens().starts_with("Each"));
        }
    }
}
