use std::cmp::Ordering;

use super::SortColumn;
use crate::domain::{AutonomyLevel, Bot, BotStatus};

/// The Bots table's columns a person can sort by.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BotColumn {
    Status,
    Bot,
    Level,
    Team,
    RunsOn,
    StartsBy,
}

impl SortColumn for BotColumn {
    type Row = Bot;

    fn label(self) -> &'static str {
        match self {
            Self::Status => "Status",
            Self::Bot => "Bot",
            Self::Level => "Level",
            Self::Team => "Team",
            Self::RunsOn => "Runs on",
            Self::StartsBy => "Starts by",
        }
    }

    fn compare(self, a: &Bot, b: &Bot) -> Ordering {
        match self {
            Self::Status => rank(a.status).cmp(&rank(b.status)),
            Self::Bot => a.id.cmp(&b.id),
            Self::Level => level(a.level).cmp(&level(b.level)),
            Self::Team => a.team_summary().cmp(&b.team_summary()),
            Self::RunsOn => a.placement.describe().cmp(&b.placement.describe()),
            Self::StartsBy => a.trigger_summary().cmp(&b.trigger_summary()),
        }
    }
}

/// On first, then paused, then the ones never switched on.
fn rank(status: BotStatus) -> u8 {
    match status {
        BotStatus::Enabled => 0,
        BotStatus::Paused => 1,
        BotStatus::Registered => 2,
        BotStatus::Draft => 3,
        BotStatus::Unknown => 4,
    }
}

/// From the level that may do the least alone to the one that may do most.
fn level(level: AutonomyLevel) -> u8 {
    match level {
        AutonomyLevel::Observe => 0,
        AutonomyLevel::Propose => 1,
        AutonomyLevel::ActWithApproval => 2,
        AutonomyLevel::ActWithinPolicy => 3,
        AutonomyLevel::Unknown => 4,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};
    use crate::ui::table::{SortDirection, SortSpec};

    fn bots() -> Vec<Bot> {
        let (cp, _) = fleet();
        let mut bots = run(cp.list_bots()).unwrap();
        bots[0].status = BotStatus::Paused;
        bots[1].groups = vec!["ops".into()];
        bots
    }

    fn ids(spec: SortSpec<BotColumn>, bots: &[Bot]) -> Vec<String> {
        spec.order(bots).into_iter().map(|b| b.id.to_string()).collect()
    }

    #[test]
    fn sorts_bots_by_status_level_and_name() {
        let bots = bots();
        assert_eq!(
            ids(SortSpec::by(BotColumn::Bot, SortDirection::Descending), &bots),
            vec!["bot-wrx80", "bot-act-with-approval", "bot"]
        );
        assert_eq!(
            ids(SortSpec::by(BotColumn::Status, SortDirection::Ascending), &bots)[2],
            bots[0].id.to_string()
        );
        assert_eq!(
            ids(
                SortSpec::by(BotColumn::Level, SortDirection::Descending)
                    .then(BotColumn::Bot, SortDirection::Ascending),
                &bots
            ),
            vec!["bot-act-with-approval", "bot", "bot-wrx80"]
        );
        assert_eq!(
            ids(SortSpec::by(BotColumn::Team, SortDirection::Descending), &bots)[0],
            bots[1].id.to_string()
        );
        let by_place = ids(SortSpec::by(BotColumn::RunsOn, SortDirection::Ascending), &bots);
        assert_eq!(by_place.len(), 3);
        let by_trigger = ids(SortSpec::by(BotColumn::StartsBy, SortDirection::Ascending), &bots);
        assert_eq!(by_trigger.len(), 3);
        assert_eq!(BotColumn::RunsOn.label(), "Runs on");
    }

    #[test]
    fn every_status_and_level_has_a_place() {
        let statuses = [
            BotStatus::Enabled,
            BotStatus::Paused,
            BotStatus::Registered,
            BotStatus::Draft,
            BotStatus::Unknown,
        ];
        assert!(statuses.windows(2).all(|w| rank(w[0]) < rank(w[1])));
        let levels = [
            AutonomyLevel::Observe,
            AutonomyLevel::Propose,
            AutonomyLevel::ActWithApproval,
            AutonomyLevel::ActWithinPolicy,
            AutonomyLevel::Unknown,
        ];
        assert!(levels.windows(2).all(|w| level(w[0]) < level(w[1])));
        for column in [
            BotColumn::Status,
            BotColumn::Bot,
            BotColumn::Level,
            BotColumn::Team,
            BotColumn::StartsBy,
        ] {
            assert!(!column.label().is_empty());
        }
    }
}
