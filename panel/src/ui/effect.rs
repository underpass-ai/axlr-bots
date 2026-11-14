use std::path::PathBuf;

use super::{LiveStream, Screen};
use crate::application::{
    BotEdit, BulkAction, BulkTarget, ChatTurn, EventFeed, Notice, PanelConfig, PanelSettings, SubscriptionChange,
};
use crate::domain::{AutonomyLevel, BotForm, BotId, Decision, ExecutionId, IdempotencyKey, NodeId, TaskId};

/// What the panel asks the outside world to do after a message: a call
/// to the control plane, a dialog, the clipboard. The update logic only
/// returns effects; the runtime carries them out.
#[derive(Debug, Clone, PartialEq)]
pub enum Effect {
    /// The page on show back at its top: another screen opened.
    ScrollTop,
    Refresh {
        generation: u64,
    },
    LoadTask(TaskId),
    LoadActivity(TaskId),
    FollowActivity {
        id: TaskId,
        cursor: Option<String>,
        wait: u32,
    },
    /// Read a task's timeline after the feed, waiting up to `wait` seconds.
    FollowTask {
        id: TaskId,
        feed: EventFeed,
        wait: u32,
    },
    /// Bring the end of a box of the running task's Activity into view.
    ShowEnd(LiveStream),
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
    /// Apply the change the bot holds staged.
    ApplyStaged(BotId),
    /// Save the edit of a bot's mission and tools, unless it changed since.
    SaveBotEdit(BotId, BotEdit),
    /// Read what deleting a bot will do, then delete it.
    PreviewDelete(BotId),
    DeleteBot(BotId),
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
    /// The decision, and the call it was made on when the panel knew it.
    Decide(TaskId, Decision, Option<String>),
    /// Fetch an artifact and ask where to save it.
    Download(ExecutionId, String),
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
    /// Keep the look and the New bot defaults; the control plane
    /// section stays as saved.
    SavePreferences {
        settings: PanelSettings,
        quiet: bool,
    },
    /// Write the settings to a file the operator picks.
    ExportSettings(PanelSettings),
    /// Read settings from a file the operator picks.
    ImportSettings,
    /// One act on many rows: a call each, a few at a time.
    Bulk {
        action: BulkAction,
        targets: Vec<BulkTarget>,
    },
}
