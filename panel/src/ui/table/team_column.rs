use std::cmp::Ordering;

use super::SortColumn;
use crate::domain::Team;

/// The Teams table's columns a person can sort by.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TeamColumn {
    Team,
    /// How many bots are in it.
    Bots,
}

impl SortColumn for TeamColumn {
    type Row = Team;

    fn label(self) -> &'static str {
        match self {
            Self::Team => "Team",
            Self::Bots => "Bots",
        }
    }

    fn compare(self, a: &Team, b: &Team) -> Ordering {
        match self {
            Self::Team => a.name.cmp(&b.name),
            Self::Bots => a.members.len().cmp(&b.members.len()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};
    use crate::ui::table::{SortDirection, SortSpec};

    #[test]
    fn sorts_teams_by_size_then_name() {
        let (cp, _) = fleet();
        let mut bots = run(cp.list_bots()).unwrap();
        bots[0].groups = vec!["qa".into(), "ops".into()];
        bots[1].groups = vec!["ops".into()];
        bots[2].groups = vec!["lab".into()];
        let teams = Team::from_bots(&bots);
        let spec =
            SortSpec::by(TeamColumn::Bots, SortDirection::Descending).then(TeamColumn::Team, SortDirection::Ascending);
        let names: Vec<&str> = spec.order(&teams).into_iter().map(|t| t.name.as_str()).collect();
        assert_eq!(names, vec!["ops", "lab", "qa"]);
        assert_eq!((TeamColumn::Team.label(), TeamColumn::Bots.label()), ("Team", "Bots"));
    }
}
