use std::sync::Arc;

use super::{ControlPlane, EventFeed, PanelError};
use crate::domain::BotId;

/// How many events a followed bot keeps on screen.
pub const FEED_SIZE: usize = 200;

/// Follows a bot's events: each call reads the newest and adds those after
/// the feed's cursor.
pub struct FollowBotEvents {
    control_plane: Arc<dyn ControlPlane>,
}

impl FollowBotEvents {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: BotId, mut feed: EventFeed) -> Result<EventFeed, PanelError> {
        let read = self.control_plane.bot_events(id, FEED_SIZE as u32).await?;
        feed.merge(read, FEED_SIZE);
        Ok(feed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::SwitchBot;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn each_read_adds_the_new_events() {
        let (control_plane, _) = fleet();
        let id = BotId::new("bot").unwrap();
        let uc = FollowBotEvents::new(control_plane.clone());
        let feed = run(uc.execute(id.clone(), EventFeed::default())).unwrap();
        let first = feed.events.len();
        assert!(first > 0 && feed.new == first);
        run(SwitchBot::new(control_plane.clone()).pause(id.clone())).unwrap();
        let feed = run(uc.execute(id.clone(), feed)).unwrap();
        assert_eq!(feed.new, 1);
        assert_eq!(feed.events.last().unwrap().event_type, "bot.paused");
        assert!(matches!(
            run(uc.execute(BotId::new("ghost").unwrap(), feed)),
            Err(PanelError::NotFound(_))
        ));
    }
}
