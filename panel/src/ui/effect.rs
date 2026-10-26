use std::path::PathBuf;

use crate::application::{ChatTurn, EventFeed, PanelConfig};
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
    Recall {
        bot: BotId,
        key: IdempotencyKey,
    },
    SetTeams(BotId, Vec<String>),
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
    Connect {
        config: PanelConfig,
        save: bool,
    },
}
