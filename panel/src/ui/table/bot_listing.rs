use std::collections::BTreeMap;

use super::{BotGroup, BotShow};
use crate::domain::{Bot, BotStatus};

/// What the Bots list shows of the fleet's bots: the chip that is on,
/// and which teams are open. A team is met open when one of its bots is
/// on and closed when none is; from then on only the person opens or
/// closes it, so a list never folds under the hand that pauses its last
/// bot.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BotListing {
    pub show: BotShow,
    /// Each team met so far, and whether it is open.
    open: BTreeMap<String, bool>,
}

impl BotListing {
    /// The fleet as a refresh read it: a team seen for the first time
    /// starts open when one of its bots is on, closed when none is.
    pub fn meet(&mut self, bots: &[Bot]) {
        for group in self.whole().groups(bots) {
            self.open.entry(group.team).or_insert(group.open);
        }
    }

    /// A team's header pressed: open it when it is closed, close it
    /// when it is open.
    pub fn fold(&mut self, team: &str, bots: &[Bot]) {
        let open = self
            .whole()
            .groups(bots)
            .into_iter()
            .find(|g| g.team == team)
            .is_some_and(|g| g.open);
        self.open.insert(team.to_string(), !open);
    }

    /// This listing with every bot let through: a team keeps its place
    /// and its fold whatever chip is on.
    fn whole(&self) -> Self {
        Self {
            show: BotShow::All,
            open: self.open.clone(),
        }
    }

    /// The bots by team, the teams with a bot on first and then by
    /// name, the bots in no team last. A bot in two teams is listed in
    /// both. A team none of whose bots the chip lets through is left
    /// out. `bots` come in the table's order and keep it.
    pub fn groups<'a>(&self, bots: impl IntoIterator<Item = &'a Bot>) -> Vec<BotGroup<'a>> {
        let mut groups: Vec<BotGroup<'a>> = Vec::new();
        for bot in bots {
            let none = [String::new()];
            let teams = if bot.groups.is_empty() {
                &none[..]
            } else {
                &bot.groups[..]
            };
            for team in teams {
                let at = match groups.iter().position(|g| &g.team == team) {
                    Some(at) => at,
                    None => {
                        groups.push(BotGroup {
                            team: team.clone(),
                            on: 0,
                            paused: 0,
                            open: false,
                            bots: Vec::new(),
                        });
                        groups.len() - 1
                    }
                };
                let group = &mut groups[at];
                group.on += usize::from(bot.status == BotStatus::Enabled);
                group.paused += usize::from(bot.status == BotStatus::Paused);
                if self.show.takes(bot) {
                    group.bots.push(bot);
                }
            }
        }
        groups.retain(|g| !g.bots.is_empty());
        for group in &mut groups {
            group.open = self.open.get(&group.team).copied().unwrap_or(group.on > 0);
        }
        groups.sort_by(|a, b| (a.team.is_empty(), a.on == 0, &a.team).cmp(&(b.team.is_empty(), b.on == 0, &b.team)));
        groups
    }

    /// The bots whose rows are on show: the ones the chip lets through
    /// in the open teams, each once.
    pub fn shown<'a>(&self, bots: impl IntoIterator<Item = &'a Bot>) -> Vec<&'a Bot> {
        let mut shown: Vec<&Bot> = Vec::new();
        for group in self.groups(bots).into_iter().filter(|g| g.open) {
            for bot in group.bots {
                if !shown.iter().any(|b| b.id == bot.id) {
                    shown.push(bot);
                }
            }
        }
        shown
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bot(id: &str, groups: &[&str], status: &str) -> Bot {
        serde_json::from_value(serde_json::json!({
            "id": id, "name": id, "mission": "Do it.", "groups": groups, "status": status,
            "createdAt": "2026-10-26T10:00:00Z", "updatedAt": "2026-10-26T10:00:00Z"
        }))
        .unwrap()
    }

    fn fleet() -> Vec<Bot> {
        vec![
            bot("alone", &[], "Enabled"),
            bot("docs-a", &["docs"], "Paused"),
            bot("docs-b", &["docs"], "Paused"),
            bot("ops-a", &["ops"], "Enabled"),
            bot("ops-b", &["ops", "docs"], "Paused"),
        ]
    }

    fn names<'a>(groups: &'a [BotGroup<'_>]) -> Vec<(&'a str, bool, usize)> {
        groups.iter().map(|g| (g.name(), g.open, g.bots.len())).collect()
    }

    #[test]
    fn groups_by_team_and_a_team_with_no_bot_on_starts_closed() {
        let bots = fleet();
        let listing = BotListing::default();
        let groups = listing.groups(&bots);
        assert_eq!(
            names(&groups),
            vec![("ops", true, 2), ("docs", false, 3), ("no team", true, 1)]
        );
        assert_eq!(groups[0].counts(), "1 on · 1 paused");
        assert_eq!(groups[1].counts(), "3 paused");
        let shown: Vec<&str> = listing.shown(&bots).iter().map(|b| b.id.as_str()).collect();
        assert_eq!(shown, vec!["ops-a", "ops-b", "alone"], "a closed team shows no row");
    }

    #[test]
    fn a_press_folds_a_team_and_the_chip_narrows_them() {
        let bots = fleet();
        let mut listing = BotListing::default();
        listing.fold("docs", &bots);
        listing.fold("ops", &bots);
        assert_eq!(
            names(&listing.groups(&bots)),
            vec![("ops", false, 2), ("docs", true, 3), ("no team", true, 1)]
        );
        // A bot in two teams shows once among the rows.
        listing.fold("ops", &bots);
        assert_eq!(listing.shown(&bots).len(), 5);
        listing.fold("docs", &bots);
        listing.show = BotShow::On;
        let groups = listing.groups(&bots);
        assert_eq!(names(&groups), vec![("ops", true, 1), ("no team", true, 1)]);
        assert_eq!(
            groups[0].counts(),
            "1 on · 1 paused",
            "the header counts the whole team"
        );
        // A chip that hides a team's rows does not change what a press
        // on its header does once they are back.
        listing.fold("docs", &bots);
        listing.show = BotShow::Paused;
        assert_eq!(names(&listing.groups(&bots)), vec![("ops", true, 1), ("docs", true, 3)]);
        assert_eq!(listing.shown(&bots).len(), 3);
    }

    #[test]
    fn a_team_met_open_stays_open_when_its_last_bot_is_paused() {
        let mut bots = fleet();
        let mut listing = BotListing::default();
        listing.meet(&bots);
        for bot in &mut bots {
            bot.status = BotStatus::Paused;
        }
        listing.meet(&bots);
        assert_eq!(
            names(&listing.groups(&bots)),
            vec![("docs", false, 3), ("ops", true, 2), ("no team", true, 1)],
            "none folds under the person; they only sort after the teams at work"
        );
        // A team that comes later is met as it is then.
        bots.push(bot("new-a", &["new"], "Paused"));
        listing.meet(&bots);
        assert!(!listing.groups(&bots).iter().find(|g| g.team == "new").unwrap().open);
    }
}
