use serde::{Deserialize, Serialize};

/// What a heard event does to the bot that hears it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum DeliveryMode {
    /// A new task.
    Task,
    /// A line in its running task; a task when it runs nothing.
    Message,
    /// Kept for its next task's prompt; no task.
    Note,
}

impl DeliveryMode {
    pub const ALL: [DeliveryMode; 3] = [DeliveryMode::Task, DeliveryMode::Message, DeliveryMode::Note];

    pub fn name(self) -> &'static str {
        match self {
            Self::Task => "task",
            Self::Message => "message",
            Self::Note => "note",
        }
    }

    pub fn parse(raw: &str) -> Option<Self> {
        Self::ALL.into_iter().find(|m| m.name() == raw)
    }

    /// The mode of a subscription that names none, by the kind of the
    /// topic's first segment: stop, danger and error a task, warn a
    /// message, anything else a note.
    pub fn by_kind(topic: &str) -> Self {
        let first = topic.split('.').next().unwrap_or_default();
        match first {
            "stop" | "danger" | "error" => Self::Task,
            "warn" => Self::Message,
            _ if topic == "recall" => Self::Task,
            _ => Self::Note,
        }
    }
}

impl std::fmt::Display for DeliveryMode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.name())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_kind_sets_the_default() {
        assert_eq!(DeliveryMode::by_kind("stop.recall"), DeliveryMode::Task);
        assert_eq!(DeliveryMode::by_kind("danger.*"), DeliveryMode::Task);
        assert_eq!(DeliveryMode::by_kind("error.*"), DeliveryMode::Task);
        assert_eq!(DeliveryMode::by_kind("recall"), DeliveryMode::Task);
        assert_eq!(DeliveryMode::by_kind("warn.stock"), DeliveryMode::Message);
        assert_eq!(DeliveryMode::by_kind("pedido.*"), DeliveryMode::Note);
        assert_eq!(DeliveryMode::by_kind("done.*"), DeliveryMode::Note);
        assert_eq!(DeliveryMode::parse("note"), Some(DeliveryMode::Note));
        assert_eq!(DeliveryMode::parse("loud"), None);
        assert_eq!(DeliveryMode::Message.to_string(), "message");
    }
}
