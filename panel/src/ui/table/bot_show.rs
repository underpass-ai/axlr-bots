use crate::domain::{Bot, BotStatus};

/// Which bots the Bots list shows: every one, the ones that are on, or
/// the paused ones. The chips over the list.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Hash)]
pub enum BotShow {
    #[default]
    All,
    On,
    Paused,
}

impl BotShow {
    pub const ALL: [BotShow; 3] = [BotShow::All, BotShow::On, BotShow::Paused];

    /// The word on its chip.
    pub fn label(self) -> &'static str {
        match self {
            Self::All => "All",
            Self::On => "On",
            Self::Paused => "Paused",
        }
    }

    /// Whether the bot is one of those it shows.
    pub fn takes(self, bot: &Bot) -> bool {
        match self {
            Self::All => true,
            Self::On => bot.status == BotStatus::Enabled,
            Self::Paused => bot.status == BotStatus::Paused,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn each_chip_takes_its_bots() {
        let (cp, _) = fleet();
        let mut bots = run(cp.list_bots()).unwrap();
        bots[0].status = BotStatus::Paused;
        bots[1].status = BotStatus::Draft;
        let count = |show: BotShow| bots.iter().filter(|b| show.takes(b)).count();
        assert_eq!(count(BotShow::All), bots.len());
        assert_eq!(count(BotShow::Paused), 1);
        assert_eq!(count(BotShow::On), bots.len() - 2);
        assert_eq!(BotShow::default(), BotShow::All);
        for show in BotShow::ALL {
            assert!(!show.label().is_empty());
        }
    }
}
