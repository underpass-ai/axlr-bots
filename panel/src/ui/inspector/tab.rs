use super::{BotTab, TaskTab, TeamTab};

/// A tab of the inspector, of whichever kind of row it shows: what a
/// press on a tab sends.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Tab {
    Task(TaskTab),
    Bot(BotTab),
    Team(TeamTab),
}

impl Tab {
    /// Every tab of its kind of row, in the order shown.
    pub fn all(self) -> Vec<Tab> {
        match self {
            Self::Task(_) => TaskTab::ALL.into_iter().map(Self::Task).collect(),
            Self::Bot(_) => BotTab::ALL.into_iter().map(Self::Bot).collect(),
            Self::Team(_) => TeamTab::ALL.into_iter().map(Self::Team).collect(),
        }
    }

    /// The tab's words.
    pub fn label(self) -> &'static str {
        match self {
            Self::Task(tab) => tab.label(),
            Self::Bot(tab) => tab.label(),
            Self::Team(tab) => tab.label(),
        }
    }
}
