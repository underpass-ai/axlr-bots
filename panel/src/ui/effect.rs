use std::path::PathBuf;

use super::Screen;
use crate::application::{ChatTurn, EventFeed, Notice, PanelConfig, SubscriptionChange};
use crate::domain::{AutonomyLevel, BotForm, BotId, Decision, IdempotencyKey, NodeId, TaskId};

/// What the panel asks the outside world to do after a message: a call
/// to the control plane, a dialog, the clipboard. The update logic only
/// returns effects; the runtime carries them out.
#[derive(Debug, Clone, PartialEq)]
pub enum Effect {
    Refresh {
        generation: u64,
    },
    LoadTask(TaskId),
    FollowBot(BotId, EventFeed),
    /// Read a team's timeline after the feed, waiting up to `wait` seconds.
    FollowTeam {
        name: String,
        feed: EventFeed,
        wait: u32,
    },
    /// An urgent order: the words become an urgent task.
    Order {
        bot: BotId,
        objective: String,
        key: IdempotencyKey,
    },
    SetTeams(BotId, Vec<String>),
    /// A change of a bot's topics: one PATCH of the whole list.
    SaveSubscriptions(BotId, SubscriptionChange),
    LoadTopics(String),
    Publish {
        team: String,
        name: String,
        message: String,
        to: String,
        data: Option<serde_json::Value>,
        key: IdempotencyKey,
        /// Pressed as a shortcut: its answer leaves the form alone.
        shortcut: bool,
    },
    Mint {
        node_id: String,
        labels: String,
        ttl: String,
    },
    Revoke(NodeId),
    RegisterForm(BotForm),
    PickFile,
    RegisterFile(PathBuf),
    Enable(BotId),
    Pause(BotId),
    SetLevel(BotId, AutonomyLevel),
    StopBot(BotId),
    /// A message to a bot from its chat, with the turns before it.
    SendChat {
        bot: BotId,
        message: String,
        earlier: Vec<ChatTurn>,
        /// The task the bot runs now, which takes the message first.
        running: Option<TaskId>,
    },
    Submit {
        bot: BotId,
        objective: String,
        key: IdempotencyKey,
        urgent: bool,
    },
    Cancel(TaskId),
    Decide(TaskId, Decision),
    Copy(String),
    /// A desktop notice, and the place a click on it opens.
    Notify {
        notice: Notice,
        open: Screen,
    },
    /// Bring the panel's window to the front.
    FocusWindow,
    Connect {
        config: PanelConfig,
        save: bool,
    },
}
