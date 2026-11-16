/// A tab of a bot in the inspector: what it does now and the chat, its
/// work, its mission, its rules and tools, what it listens to and emits.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum BotTab {
    #[default]
    Now,
    Work,
    Mission,
    Rules,
    Listens,
}

impl BotTab {
    /// Every tab, in the order shown.
    pub const ALL: [Self; 5] = [Self::Now, Self::Work, Self::Mission, Self::Rules, Self::Listens];

    /// The tab's words.
    pub fn label(self) -> &'static str {
        match self {
            Self::Now => "Now",
            Self::Work => "Work",
            Self::Mission => "Mission",
            Self::Rules => "Rules & tools",
            Self::Listens => "Listens & emits",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opens_on_now_and_names_each_tab() {
        assert_eq!(BotTab::default(), BotTab::Now);
        let labels: Vec<&str> = BotTab::ALL.into_iter().map(BotTab::label).collect();
        assert_eq!(labels, ["Now", "Work", "Mission", "Rules & tools", "Listens & emits"]);
    }
}
