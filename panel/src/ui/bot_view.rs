use crate::application::{ChatTurn, EventFeed};
use crate::domain::Bot;

/// The bot on screen and its events as they are followed.
#[derive(Debug, Clone, PartialEq)]
pub struct BotView {
    pub bot: Bot,
    pub feed: EventFeed,
    /// The newest turns of its chat, oldest first.
    pub chat: Vec<ChatTurn>,
}
