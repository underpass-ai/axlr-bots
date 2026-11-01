use std::sync::Arc;

use super::{ControlPlane, EventFeed, PanelError};

/// How many events a followed team keeps on screen.
pub const TEAM_FEED_SIZE: usize = 300;

/// How long a read of a team's timeline waits for a new event.
pub const TEAM_WAIT_SECONDS: u32 = 20;

/// Follows a team's timeline: each call reads what came after the feed's
/// cursor, waiting up to `wait_seconds` for something new, and adds it.
pub struct FollowTeamEvents {
    control_plane: Arc<dyn ControlPlane>,
}

impl FollowTeamEvents {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, name: String, mut feed: EventFeed, wait_seconds: u32) -> Result<EventFeed, PanelError> {
        let after = feed.cursor().map(str::to_string);
        let read = self.control_plane.group_events(name, after, wait_seconds).await?;
        let new = read.len();
        feed.events.extend(read);
        if feed.events.len() > TEAM_FEED_SIZE {
            let excess = feed.events.len() - TEAM_FEED_SIZE;
            feed.events.drain(..excess);
        }
        feed.new = new;
        Ok(feed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::FakeControlPlane;
    use crate::application::test_support::run;

    #[test]
    fn reads_what_came_after_the_cursor() {
        let control_plane = Arc::new(FakeControlPlane::sample("2026-10-26T10:00:00Z".parse().unwrap()));
        control_plane.set_groups("bot", &["ops"]);
        control_plane.set_groups("bot-wrx80", &["ops"]);
        let uc = FollowTeamEvents::new(control_plane.clone());
        let feed = run(uc.execute("ops".into(), EventFeed::default(), 0)).unwrap();
        let first = feed.events.len();
        assert!(first > 0 && feed.new == first);
        control_plane.publish(
            "bot.event.emitted",
            "bot",
            serde_json::json!({"name": "stop.halt", "severity": "warning", "message": "a deploy is starting", "to": ["bot-wrx80"]}),
        );
        let feed = run(uc.execute("ops".into(), feed, 0)).unwrap();
        assert_eq!(feed.new, 1);
        assert!(feed.events.last().unwrap().needs_attention());
        assert!(matches!(
            run(uc.execute("qa".into(), feed, 0)),
            Err(PanelError::NotFound(_))
        ));
    }
}
