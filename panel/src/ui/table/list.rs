use crate::application::BulkAction;

/// A list screen whose table has picked rows.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum List {
    Nodes,
    Bots,
    Tasks,
    Incidents,
    Teams,
}

impl List {
    /// What its rows are, counted: `3 bots`.
    pub fn count(self, n: usize) -> String {
        let (one, many) = match self {
            Self::Nodes => ("node", "nodes"),
            Self::Bots => ("bot", "bots"),
            Self::Tasks => ("task", "tasks"),
            Self::Incidents => ("incident", "incidents"),
            Self::Teams => ("team", "teams"),
        };
        format!("{n} {}", if n == 1 { one } else { many })
    }

    /// The acts its bar offers on the picked rows. A team's go to its
    /// bots.
    pub fn actions(self) -> &'static [BulkAction] {
        match self {
            Self::Nodes => &[BulkAction::Revoke],
            Self::Bots => &[
                BulkAction::TurnOn,
                BulkAction::Pause,
                BulkAction::Stop,
                BulkAction::Delete,
            ],
            Self::Teams => &[BulkAction::TurnOn, BulkAction::Pause, BulkAction::Stop],
            Self::Tasks => &[BulkAction::Approve, BulkAction::Deny, BulkAction::Cancel],
            Self::Incidents => &[BulkAction::Resolve],
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counts_its_rows() {
        assert_eq!(List::Bots.count(1), "1 bot");
        assert_eq!(List::Tasks.count(3), "3 tasks");
        assert_eq!(List::Nodes.count(0), "0 nodes");
        assert_eq!(List::Incidents.count(2), "2 incidents");
        assert_eq!(List::Teams.count(1), "1 team");
    }

    #[test]
    fn offers_the_acts_that_fit_its_rows() {
        assert_eq!(List::Nodes.actions(), &[BulkAction::Revoke]);
        assert_eq!(List::Incidents.actions(), &[BulkAction::Resolve]);
        assert!(List::Bots.actions().contains(&BulkAction::Pause));
        assert!(List::Bots.actions().contains(&BulkAction::Delete));
        assert!(!List::Teams.actions().contains(&BulkAction::Delete));
        assert!(List::Tasks.actions().contains(&BulkAction::Cancel));
    }
}
