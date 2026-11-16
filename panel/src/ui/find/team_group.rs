use super::{Collapsed, GroupKey};
use crate::domain::{Bot, BotId, BotStatus};

/// The bots that name no team go under this header.
pub const NO_TEAM: &str = "no team";

/// With more teams than this, a team with no bot on starts closed.
const TEAMS_ALL_OPEN: usize = 4;

/// The bots of one team under its header in the bot list: how many of
/// the whole team are on and how many are not, the rows the list lets
/// through, and whether the group is open. A bot of several teams is
/// listed once, under the first it names, so a row is one bot and the
/// keyboard meets each bot once.
#[derive(Debug, Clone, PartialEq)]
pub struct TeamGroup<'a> {
    /// What a press on its header names.
    pub key: GroupKey,
    /// The team's name, or `no team`.
    pub name: String,
    /// Its bots that accept tasks, shown or not.
    pub on: usize,
    /// Its bots that do not: paused, or not turned on yet.
    pub paused: usize,
    /// Whether it starts closed, before the person touches it: what a
    /// press on its header has to know.
    pub starts_closed: bool,
    /// Its rows show; closed, only its header does.
    pub open: bool,
    /// Its bots the list lets through, in the order they came.
    pub bots: Vec<&'a Bot>,
}

impl<'a> TeamGroup<'a> {
    /// The bots in their teams: every bot counts in its team's header,
    /// and the ones `shows` lets through are its rows, in the order they
    /// came. A team none of whose bots shows is left out. The team with
    /// most bots on comes first, two with as many by name. A team stands
    /// as it was met (see `meet`); one not met yet starts closed when no
    /// filter is typed, it has no bot on and there are more than four
    /// teams. A typed filter opens what only started closed; a group the
    /// person touched stays as they left it.
    pub fn of(
        bots: impl IntoIterator<Item = &'a Bot>,
        shows: impl Fn(&Bot) -> bool,
        filtering: bool,
        collapsed: &Collapsed,
    ) -> Vec<TeamGroup<'a>> {
        let mut groups: Vec<TeamGroup<'a>> = Vec::new();
        for bot in bots {
            let name = bot.groups.first().map_or(NO_TEAM, String::as_str);
            let at = match groups.iter().position(|group| group.name == name) {
                Some(at) => at,
                None => {
                    groups.push(TeamGroup {
                        key: GroupKey::Team(name.to_string()),
                        name: name.to_string(),
                        on: 0,
                        paused: 0,
                        starts_closed: false,
                        open: true,
                        bots: Vec::new(),
                    });
                    groups.len() - 1
                }
            };
            let group = &mut groups[at];
            if bot.status == BotStatus::Enabled {
                group.on += 1;
            } else {
                group.paused += 1;
            }
            if shows(bot) {
                group.bots.push(bot);
            }
        }
        let many = groups.len() > TEAMS_ALL_OPEN;
        groups.retain(|group| !group.bots.is_empty());
        for group in &mut groups {
            let first = group.on == 0 && many;
            group.starts_closed = !filtering && collapsed.met(&group.key).unwrap_or(first);
            group.open = !collapsed.is_closed(&group.key, group.starts_closed);
        }
        groups.sort_by(|a, b| b.on.cmp(&a.on).then_with(|| a.name.cmp(&b.name)));
        groups
    }

    /// The teams of the fleet as a refresh read it, met: a team seen
    /// for the first time keeps how it started, so it never opens or
    /// closes under the person's hand when its bots change.
    pub fn meet(bots: &[Bot], collapsed: &mut Collapsed) {
        for group in TeamGroup::of(bots, |_| true, false, collapsed) {
            collapsed.meet(group.key, group.starts_closed);
        }
    }

    /// What its header says after the name: `3 on · 1 paused`, `2 on`,
    /// `4 paused`.
    pub fn summary(&self) -> String {
        match (self.on, self.paused) {
            (on, 0) => format!("{on} on"),
            (0, paused) => format!("{paused} paused"),
            (on, paused) => format!("{on} on · {paused} paused"),
        }
    }

    /// The bots on show, group after group: the ones of the open
    /// groups.
    pub fn shown(groups: &[TeamGroup<'a>]) -> Vec<&'a Bot> {
        groups
            .iter()
            .filter(|group| group.open)
            .flat_map(|group| group.bots.iter().copied())
            .collect()
    }

    /// The bots on show by id: what the row cursor moves over.
    pub fn keys(groups: &[TeamGroup<'a>]) -> Vec<BotId> {
        Self::shown(groups).into_iter().map(|bot| bot.id.clone()).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::bot;
    use super::*;

    fn names(groups: &[TeamGroup<'_>]) -> Vec<String> {
        groups.iter().map(|g| g.name.clone()).collect()
    }

    fn all(_: &Bot) -> bool {
        true
    }

    fn fleet() -> Vec<Bot> {
        vec![
            bot("lab-reader", "lab", "Paused"),
            bot("ops-watcher", "ops", "Enabled"),
            bot("dev-builder", "dev", "Enabled"),
            bot("ops-fixer", "ops", "Paused"),
            bot("dev-tester", "dev", "Enabled"),
            bot("free", "", "Enabled"),
            bot("draft", "lab", "Registered"),
        ]
    }

    #[test]
    fn groups_bots_by_team_most_on_first_then_by_name() {
        let bots = fleet();
        let groups = TeamGroup::of(&bots, all, false, &Collapsed::default());
        assert_eq!(names(&groups), vec!["dev", "no team", "ops", "lab"]);
        assert_eq!(
            groups.iter().map(TeamGroup::summary).collect::<Vec<_>>(),
            vec!["2 on", "1 on", "1 on · 1 paused", "2 paused"]
        );
        assert_eq!(groups[2].key, GroupKey::Team("ops".into()));
        let ops: Vec<&str> = groups[2].bots.iter().map(|b| b.id.as_str()).collect();
        assert_eq!(
            ops,
            vec!["ops-watcher", "ops-fixer"],
            "inside a team the bots keep their order"
        );
        assert!(groups.iter().all(|g| g.open), "four teams or fewer all start open");
    }

    #[test]
    fn a_bot_of_several_teams_is_listed_once_under_the_first() {
        let bots = [
            bot("ops-watcher", "ops,lab", "Enabled"),
            bot("lab-reader", "lab", "Enabled"),
        ];
        let groups = TeamGroup::of(&bots, all, false, &Collapsed::default());
        assert_eq!(names(&groups), vec!["lab", "ops"]);
        assert_eq!(TeamGroup::keys(&groups).len(), 2);
    }

    fn many() -> Vec<Bot> {
        let mut bots = fleet();
        bots.push(bot("web-checker", "web", "Paused"));
        bots
    }

    #[test]
    fn with_many_teams_one_with_no_bot_on_starts_closed() {
        let bots = many();
        let groups = TeamGroup::of(&bots, all, false, &Collapsed::default());
        assert_eq!(names(&groups), vec!["dev", "no team", "ops", "lab", "web"]);
        assert_eq!(
            groups.iter().map(|g| g.open).collect::<Vec<_>>(),
            vec![true, true, true, false, false]
        );
        assert!(groups[3].starts_closed && !groups[0].starts_closed);
        let shown: Vec<String> = TeamGroup::keys(&groups).iter().map(|id| id.to_string()).collect();
        assert_eq!(
            shown,
            vec!["dev-builder", "dev-tester", "free", "ops-watcher", "ops-fixer"]
        );
    }

    #[test]
    fn a_typed_filter_opens_what_only_started_closed() {
        let bots = many();
        let groups = TeamGroup::of(&bots, all, true, &Collapsed::default());
        assert!(groups.iter().all(|g| g.open && !g.starts_closed));
    }

    #[test]
    fn a_group_the_person_touched_stays_as_they_left_it() {
        let bots = many();
        let mut collapsed = Collapsed::default();
        let groups = TeamGroup::of(&bots, all, false, &collapsed);
        let (lab, dev) = (&groups[3], &groups[0]);
        collapsed.toggle(lab.key.clone(), lab.starts_closed);
        collapsed.toggle(dev.key.clone(), dev.starts_closed);
        let groups = TeamGroup::of(&bots, all, false, &collapsed);
        assert!(groups[3].open, "lab started closed and the press opened it");
        assert!(!groups[0].open, "dev started open and the press closed it");
        let filtered = TeamGroup::of(&bots, all, true, &collapsed);
        assert!(
            !filtered[0].open,
            "what the person closed stays closed while they filter"
        );
        assert_eq!(TeamGroup::shown(&filtered).len(), 6);
    }

    #[test]
    fn the_header_counts_the_whole_team_and_the_rows_are_the_ones_let_through() {
        let bots = many();
        let on = |bot: &Bot| bot.status == BotStatus::Enabled;
        let groups = TeamGroup::of(&bots, on, false, &Collapsed::default());
        assert_eq!(
            names(&groups),
            vec!["dev", "no team", "ops"],
            "a team with no row let through is left out"
        );
        let ops = &groups[2];
        assert_eq!(ops.summary(), "1 on · 1 paused");
        assert_eq!(ops.bots.len(), 1);
        let paused = |bot: &Bot| bot.status == BotStatus::Paused;
        let groups = TeamGroup::of(&bots, paused, false, &Collapsed::default());
        assert_eq!(names(&groups), vec!["ops", "lab", "web"]);
        assert!(groups[0].open, "a team at work starts open whatever the chip shows");
    }

    #[test]
    fn a_team_met_open_stays_open_when_its_last_bot_is_paused() {
        let mut bots = many();
        let mut collapsed = Collapsed::default();
        TeamGroup::meet(&bots, &mut collapsed);
        for bot in &mut bots {
            bot.status = BotStatus::Paused;
        }
        TeamGroup::meet(&bots, &mut collapsed);
        let groups = TeamGroup::of(&bots, all, false, &collapsed);
        let open: Vec<(String, bool)> = groups.iter().map(|g| (g.name.clone(), g.open)).collect();
        assert_eq!(
            open,
            vec![
                ("dev".to_string(), true),
                ("lab".to_string(), false),
                ("no team".to_string(), true),
                ("ops".to_string(), true),
                ("web".to_string(), false)
            ],
            "none folds under the person; they only sort after by name"
        );
        // A team that comes later is met as it is then.
        bots.push(bot("new-a", "new", "Paused"));
        TeamGroup::meet(&bots, &mut collapsed);
        let groups = TeamGroup::of(&bots, all, false, &collapsed);
        assert!(!groups.iter().find(|g| g.name == "new").unwrap().open);
    }

    #[test]
    fn no_bots_are_no_groups() {
        let none: [Bot; 0] = [];
        assert!(TeamGroup::of(&none, all, false, &Collapsed::default()).is_empty());
        assert!(TeamGroup::shown(&[]).is_empty() && TeamGroup::keys(&[]).is_empty());
    }
}
