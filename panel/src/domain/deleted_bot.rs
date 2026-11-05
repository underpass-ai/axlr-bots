use serde::Deserialize;

use super::{Bot, BotReference};

/// The control plane's answer to a delete: the bot as it was, what it
/// cancelled, resolved and removed, and the bots that still name it.
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeletedBot {
    pub bot: Bot,
    #[serde(default)]
    pub cancelled: Vec<String>,
    #[serde(default)]
    pub children_cancelled: Vec<String>,
    #[serde(default)]
    pub incidents_resolved: Vec<String>,
    #[serde(default)]
    pub timers_removed: u32,
    #[serde(default)]
    pub notifications_dropped: u32,
    #[serde(default)]
    pub named_by: Vec<BotReference>,
}

impl DeletedBot {
    /// What the delete did, in a line: `2 tasks cancelled · 1 incident
    /// resolved`, or `nothing was running`.
    pub fn summary(&self) -> String {
        let mut parts = Vec::new();
        let tasks = self.cancelled.len() + self.children_cancelled.len();
        if tasks > 0 {
            parts.push(plural(tasks, "task") + " cancelled");
        }
        if !self.incidents_resolved.is_empty() {
            parts.push(plural(self.incidents_resolved.len(), "incident") + " resolved");
        }
        if parts.is_empty() {
            return "nothing was running".to_string();
        }
        parts.join(" · ")
    }
}

fn plural(n: usize, word: &str) -> String {
    if n == 1 {
        format!("1 {word}")
    } else {
        format!("{n} {word}s")
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
            "cancelled":["task-1"],"childrenCancelled":["task-2"],"incidentsResolved":["inc-1"],
            "timersRemoved":1,"notificationsDropped":0,"namedBy":[{"botId":"watcher","how":"delegation"}]}"#,
        )
        .unwrap();
        assert_eq!(deleted.summary(), "2 tasks cancelled · 1 incident resolved");
        deleted.children_cancelled.clear();
        deleted.incidents_resolved.clear();
        assert_eq!(deleted.summary(), "1 task cancelled");
        deleted.cancelled.clear();
        assert_eq!(deleted.summary(), "nothing was running");
    }
}
