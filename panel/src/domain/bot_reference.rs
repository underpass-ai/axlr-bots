use serde::Deserialize;

/// Another bot that still names a bot: it may delegate to it, or listens
/// to a topic from it. Deleting the bot leaves these as they are.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct BotReference {
    pub bot_id: String,
    /// `delegation` or `subscription`; another word from a newer control
    /// plane is shown as it is.
    pub how: String,
    #[serde(default)]
    pub topic: String,
}

impl BotReference {
    /// What the other bot does with it, in words: `watcher may ask it
    /// for a task`, `worker-2 listens to warn.* from it`.
    pub fn describe(&self) -> String {
        match self.how.as_str() {
            "delegation" => format!("{} may ask it for a task", self.bot_id),
            "subscription" => format!("{} listens to {} from it", self.bot_id, self.topic),
            other => format!("{} names it ({other})", self.bot_id),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_how_another_bot_names_it() {
        let read = |json: &str| serde_json::from_str::<BotReference>(json).unwrap().describe();
        assert_eq!(
            read(r#"{"botId":"watcher","how":"delegation"}"#),
            "watcher may ask it for a task"
        );
        assert_eq!(
            read(r#"{"botId":"worker-2","how":"subscription","topic":"warn.*"}"#),
            "worker-2 listens to warn.* from it"
        );
        assert_eq!(read(r#"{"botId":"x","how":"team"}"#), "x names it (team)");
    }
}
