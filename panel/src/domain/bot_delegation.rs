use serde::{Deserialize, Serialize};

/// The part of a bot's delegation the panel reads: the bots outside its
/// teams it may ask to do a task.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct BotDelegation {
    #[serde(default)]
    pub to: Vec<String>,
}
