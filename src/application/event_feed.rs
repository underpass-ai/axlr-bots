use crate::domain::Event;

/// A bot's events as they are followed: what was read so far and how many
/// of the last read were new.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct EventFeed {
    pub events: Vec<Event>,
    pub new: usize,
}

impl EventFeed {
    /// The id of the newest event read, the cursor of the next read.
    pub fn cursor(&self) -> Option<&str> {
        self.events.last().map(|e| e.event_id.as_str())
    }

    /// Adds what a read returned after the cursor, keeping at most `keep`.
    pub fn merge(&mut self, read: Vec<Event>, keep: usize) {
        let start = match self.cursor() {
            Some(cursor) => read
                .iter()
                .position(|e| e.event_id == cursor)
                .map(|i| i + 1)
                .unwrap_or(0),
            None => 0,
        };
        let fresh: Vec<Event> = read.into_iter().skip(start).collect();
        self.new = fresh.len();
        self.events.extend(fresh);
        if self.events.len() > keep {
            let excess = self.events.len() - keep;
            self.events.drain(..excess);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn event(id: &str) -> Event {
        serde_json::from_value(serde_json::json!({
            "eventId": id, "type": "bot.task.submitted", "tenantId": "t", "botId": "b", "createdAt": "2026-10-25T10:00:00Z"
        }))
        .unwrap()
    }

    #[test]
    fn adds_what_came_after_the_cursor() {
        let mut feed = EventFeed::default();
        assert_eq!(feed.cursor(), None);
        feed.merge(vec![event("a"), event("b")], 10);
        assert_eq!((feed.events.len(), feed.new, feed.cursor()), (2, 2, Some("b")));
        feed.merge(vec![event("a"), event("b"), event("c")], 10);
        assert_eq!((feed.events.len(), feed.new, feed.cursor()), (3, 1, Some("c")));
        feed.merge(vec![event("b"), event("c")], 10);
        assert_eq!(feed.new, 0);
        // The cursor fell out of the read window: everything read is new.
        feed.merge(vec![event("x"), event("y")], 4);
        assert_eq!(feed.new, 2);
        assert_eq!(
            feed.events.iter().map(|e| e.event_id.as_str()).collect::<Vec<_>>(),
            vec!["b", "c", "x", "y"]
        );
    }
}
