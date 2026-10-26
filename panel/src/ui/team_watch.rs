use std::collections::{BTreeMap, BTreeSet};

use crate::application::EventFeed;
use crate::domain::BotId;

/// The panel's watch over the teams: each team's timeline as read so
/// far, the teams with a read on its way, and a bot's team being edited.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct TeamWatch {
    pub feeds: BTreeMap<String, EventFeed>,
    pub following: BTreeSet<String>,
    /// The bot whose team is being changed, and the words typed so far.
    pub editing: Option<(BotId, String)>,
}

impl TeamWatch {
    /// A team's timeline, empty before its first read.
    pub fn feed(&self, team: &str) -> Option<&EventFeed> {
        self.feeds.get(team)
    }
}
