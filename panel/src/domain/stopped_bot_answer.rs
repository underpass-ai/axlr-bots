use serde::Deserialize;

use super::{Bot, StopRefusal};

/// The control plane's answer to a stop: the bot, paused, the tasks it
/// cancelled and the ones it could not.
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StoppedBotAnswer {
    pub bot: Bot,
    #[serde(default)]
    pub cancelled: Vec<String>,
    #[serde(default)]
    pub not_cancelled: Vec<StopRefusal>,
}
