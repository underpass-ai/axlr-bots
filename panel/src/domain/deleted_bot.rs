use serde::Deserialize;

use super::{Bot, BotReference};

/// The control plane's answer to a delete: the bot as it was, what it
/// cancelled and removed, and the bots that still name it.
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeletedBot {
    pub bot: Bot,
    #[serde(default)]
    pub cancelled: Vec<String>,
    #[serde(default)]
    pub children_cancelled: Vec<String>,
    #[serde(default)]
    pub timers_removed: u32,
    #[serde(default)]
    pub notifications_dropped: u32,
    #[serde(default)]
    pub named_by: Vec<BotReference>,
}

impl DeletedBot {
    /// What the delete did, in a line: `2 tasks cancelled`, or `nothing
    /// was running`.
    pub fn summary(&self) -> String {
        match self.cancelled.len() + self.children_cancelled.len() {
            0 => "nothing was running".to_string(),
            1 => "1 task cancelled".to_string(),
            n => format!("{n} tasks cancelled"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_answer_and_sums_it_up() {
        let mut deleted: DeletedBot = serde_json::from_str(
            r#"{"bot":{"id":"worker-1","name":"worker-1","mission":"m","status":"Enabled",
            "createdAt":"2026-11-05T10:00:00Z","updatedAt":"2026-11-05T10:00:00Z"},
            "cancelled":["task-1"],"childrenCancelled":["task-2"],
            "timersRemoved":1,"notificationsDropped":0,"namedBy":[{"botId":"watcher","how":"delegation"}]}"#,
        )
        .unwrap();
        assert_eq!(deleted.summary(), "2 tasks cancelled");
        deleted.children_cancelled.clear();
        assert_eq!(deleted.summary(), "1 task cancelled");
        deleted.cancelled.clear();
        assert_eq!(deleted.summary(), "nothing was running");
    }
}
