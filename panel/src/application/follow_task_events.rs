use std::sync::Arc;

use super::{ControlPlane, EventFeed, PanelError};
use crate::domain::TaskId;

/// How many events a followed task keeps: its whole timeline, as the
/// control plane answers it at most.
pub const LIVE_FEED_SIZE: usize = 1000;

/// How long a read of a running task's timeline waits for a new event.
pub const LIVE_WAIT_SECONDS: u32 = 20;

/// Follows a task's timeline: each call reads what came after the feed's
/// cursor, waiting up to `wait_seconds` for something new, and adds it.
/// A control plane older than `after` answers the newest events again;
/// the cursor drops those already read.
pub struct FollowTaskEvents {
    control_plane: Arc<dyn ControlPlane>,
}

impl FollowTaskEvents {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: TaskId, mut feed: EventFeed, wait_seconds: u32) -> Result<EventFeed, PanelError> {
        let after = feed.cursor().map(str::to_string);
        let read = self.control_plane.task_events_after(id, after, wait_seconds).await?;
        feed.merge(read, LIVE_FEED_SIZE);
        Ok(feed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{TaskFilter, TaskStatus};

    #[test]
    fn reads_what_came_after_the_cursor() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let running = tasks.into_iter().find(|t| t.status == TaskStatus::Running).unwrap().id;
        let uc = FollowTaskEvents::new(control_plane.clone());
        let feed = run(uc.execute(running.clone(), EventFeed::default(), 0)).unwrap();
        let first = feed.events.len();
        assert!(first > 0 && feed.new == first);
        let again = run(uc.execute(running.clone(), feed, LIVE_WAIT_SECONDS)).unwrap();
        assert_eq!((again.events.len(), again.new), (first, 0));
        control_plane.say(&running, "Still at it.");
        let feed = run(uc.execute(running.clone(), again, 0)).unwrap();
        assert_eq!(feed.new, 1);
        assert_eq!(feed.events.last().unwrap().event_type, "bot.task.said");
        assert!(matches!(
            run(uc.execute(TaskId::new("task-none").unwrap(), feed, 0)),
            Err(PanelError::NotFound(_))
        ));
    }
}
