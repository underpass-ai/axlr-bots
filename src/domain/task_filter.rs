use chrono::{DateTime, Utc};

use super::{BotId, TaskStatus};

/// Which tasks a list shows: one bot or all, one status or all, created
/// since a moment or ever, and how many at most.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaskFilter {
    pub bot: Option<BotId>,
    pub status: Option<TaskStatus>,
    pub since: Option<DateTime<Utc>>,
    pub limit: u32,
}

impl TaskFilter {
    /// The most a list asks for.
    pub const MAX_LIMIT: u32 = 1000;

    pub fn with_limit(limit: u32) -> Self {
        Self {
            bot: None,
            status: None,
            since: None,
            limit: limit.min(Self::MAX_LIMIT),
        }
    }
}

impl Default for TaskFilter {
    fn default() -> Self {
        Self::with_limit(100)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn caps_the_limit() {
        assert_eq!(TaskFilter::default().limit, 100);
        assert_eq!(TaskFilter::with_limit(5000).limit, 1000);
        assert!(TaskFilter::default().bot.is_none());
    }
}
