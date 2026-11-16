use super::{BotTab, Tab, TaskTab, TeamTab};

/// The tab open for each kind of row: the person who reads tasks on
/// their Answer keeps reading the next task on its Answer.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Tabs {
    pub task: TaskTab,
    pub bot: BotTab,
    pub team: TeamTab,
}

impl Tabs {
    /// A press on a tab: it is open for its kind from now on.
    pub fn open(&mut self, tab: Tab) {
        match tab {
            Tab::Task(tab) => self.task = tab,
            Tab::Bot(tab) => self.bot = tab,
            Tab::Team(tab) => self.team = tab,
        }
    }

    /// The tab after the one open for `kind`'s kind (`forward`) or before
    /// it, round from the last to the first; it is then open.
    pub fn step(&mut self, kind: Tab, forward: bool) -> Tab {
        let next = match kind {
            Tab::Task(_) => Tab::Task(turn(&TaskTab::ALL, self.task, forward)),
            Tab::Bot(_) => Tab::Bot(turn(&BotTab::ALL, self.bot, forward)),
            Tab::Team(_) => Tab::Team(turn(&TeamTab::ALL, self.team, forward)),
        };
        self.open(next);
        next
    }
}

/// The one after `now` in `all`, or before it, going round.
fn turn<T: Copy + PartialEq>(all: &[T], now: T, forward: bool) -> T {
    let at = all.iter().position(|t| *t == now).unwrap_or(0);
    let len = all.len();
    all[if forward { (at + 1) % len } else { (at + len - 1) % len }]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_kind_keeps_its_own_tab_and_the_arrows_go_round() {
        let mut tabs = Tabs::default();
        tabs.open(Tab::Task(TaskTab::Answer));
        tabs.open(Tab::Bot(BotTab::Rules));
        assert_eq!(
            (tabs.task, tabs.bot, tabs.team),
            (TaskTab::Answer, BotTab::Rules, TeamTab::Happens)
        );
        assert_eq!(tabs.step(Tab::Task(TaskTab::Activity), true), Tab::Task(TaskTab::Files));
        assert_eq!(tabs.task, TaskTab::Files);
        tabs.open(Tab::Task(TaskTab::Raw));
        assert_eq!(tabs.step(Tab::Task(TaskTab::Raw), true), Tab::Task(TaskTab::Activity));
        assert_eq!(tabs.step(Tab::Task(TaskTab::Activity), false), Tab::Task(TaskTab::Raw));
        assert_eq!(
            tabs.step(Tab::Team(TeamTab::Happens), false),
            Tab::Team(TeamTab::Members)
        );
        assert_eq!(tabs.step(Tab::Bot(BotTab::Now), true), Tab::Bot(BotTab::Listens));
        assert_eq!(Tab::Bot(BotTab::Listens).label(), "Listens & emits");
        assert_eq!(tabs.bot, BotTab::Listens);
    }
}
