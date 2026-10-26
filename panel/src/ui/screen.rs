use crate::domain::{BotId, TaskId};

/// Where the operator is.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub enum Screen {
    #[default]
    Overview,
    Nodes,
    Bots,
    Bot(BotId),
    Tasks,
    Task(TaskId),
    Teams,
    Team(String),
    Settings,
}

/// The side bar's entries; a detail belongs to its list's entry.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Section {
    Overview,
    Nodes,
    Bots,
    Teams,
    Tasks,
    Settings,
}

impl Screen {
    /// `overview`, `nodes`, `bots`, `bot:<id>`, `tasks`, `task:<id>`,
    /// `settings`: what `--screen` takes.
    pub fn parse(raw: &str) -> Option<Self> {
        match raw.split_once(':') {
            Some(("bot", id)) => BotId::new(id).ok().map(Self::Bot),
            Some(("task", id)) => TaskId::new(id).ok().map(Self::Task),
            Some(("team", name)) if !name.is_empty() => Some(Self::Team(name.to_string())),
            Some(_) => None,
            None => match raw {
                "overview" => Some(Self::Overview),
                "nodes" => Some(Self::Nodes),
                "bots" => Some(Self::Bots),
                "tasks" => Some(Self::Tasks),
                "teams" => Some(Self::Teams),
                "settings" => Some(Self::Settings),
                _ => None,
            },
        }
    }

    pub fn section(&self) -> Section {
        match self {
            Self::Overview => Section::Overview,
            Self::Nodes => Section::Nodes,
            Self::Bots | Self::Bot(_) => Section::Bots,
            Self::Tasks | Self::Task(_) => Section::Tasks,
            Self::Teams | Self::Team(_) => Section::Teams,
            Self::Settings => Section::Settings,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_the_screen_flag() {
        assert_eq!(Screen::parse("nodes"), Some(Screen::Nodes));
        assert_eq!(Screen::parse("overview"), Some(Screen::Overview));
        assert_eq!(Screen::parse("bots"), Some(Screen::Bots));
        assert_eq!(Screen::parse("tasks"), Some(Screen::Tasks));
        assert_eq!(Screen::parse("settings"), Some(Screen::Settings));
        assert_eq!(
            Screen::parse("bot:bot-wrx80"),
            Some(Screen::Bot(BotId::new("bot-wrx80").unwrap()))
        );
        assert_eq!(
            Screen::parse("task:task-1"),
            Some(Screen::Task(TaskId::new("task-1").unwrap()))
        );
        assert_eq!(Screen::parse("task:Bad Id"), None);
        assert_eq!(Screen::parse("node:x"), None);
        assert_eq!(Screen::parse("home"), None);
        assert_eq!(Screen::parse("teams"), Some(Screen::Teams));
        assert_eq!(Screen::parse("team:port"), Some(Screen::Team("port".into())));
        assert_eq!(Screen::parse("team:"), None);
        assert_eq!(Screen::Team("port".into()).section(), Section::Teams);
        assert_eq!(Screen::Task(TaskId::new("t").unwrap()).section(), Section::Tasks);
        assert_eq!(Screen::Bot(BotId::new("b").unwrap()).section(), Section::Bots);
        assert_eq!(Screen::default().section(), Section::Overview);
        assert_eq!(Screen::Nodes.section(), Section::Nodes);
        assert_eq!(Screen::Settings.section(), Section::Settings);
    }
}
