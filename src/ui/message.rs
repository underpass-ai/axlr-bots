use std::path::PathBuf;

use chrono::{DateTime, Utc};

use super::{BotChoice, BotView, Connection, NodeChoice, Screen};
use crate::application::{Fleet, PanelError, TaskDetail};
use crate::domain::{
    AutonomyLevel, Bot, BotId, CancelledTask, DecidedTask, EnrollmentToken, NodeId, RevokedNode, SubmittedTask, TaskId,
    TaskStatus,
};

/// Everything that happens in the panel: what the operator does, the
/// clock's ticks and the answers of the control plane.
#[derive(Debug, Clone)]
pub enum Message {
    Tick(DateTime<Utc>),
    Go(Screen),
    ToggleTheme,
    DismissBanner,
    Copy(String),

    FleetLoaded {
        generation: u64,
        result: Result<Fleet, PanelError>,
    },
    TaskLoaded(TaskId, Result<TaskDetail, PanelError>),
    BotLoaded(BotId, Result<BotView, PanelError>),

    MintOpen,
    MintClose,
    MintNodeId(String),
    MintLabels(String),
    MintTtl(String),
    MintSubmit,
    Minted(Result<EnrollmentToken, PanelError>),
    AskRevoke(NodeId),
    Revoked(Result<RevokedNode, PanelError>),

    RegisterOpen,
    RegisterClose,
    RegisterName(String),
    RegisterMission(String),
    RegisterLevel(AutonomyLevel),
    RegisterTools(String),
    RegisterModel(String),
    RegisterNode(NodeChoice),
    RegisterSubmit,
    PickFile,
    FilePicked(Result<Option<PathBuf>, PanelError>),
    Registered(Result<Bot, PanelError>),
    Enable(BotId),
    Pause(BotId),
    Switched(Result<Bot, PanelError>),

    FilterStatus(Option<TaskStatus>),
    FilterBot(BotChoice),
    SubmitOpen(Option<BotId>),
    SubmitClose,
    SubmitBot(BotChoice),
    SubmitObjective(String),
    SubmitSend,
    Submitted(Result<SubmittedTask, PanelError>),
    AskCancel(TaskId),
    Cancelled(Result<CancelledTask, PanelError>),
    Approve(TaskId),
    AskDeny(TaskId),
    Decided(Result<DecidedTask, PanelError>),

    ConfirmYes,
    ConfirmNo,

    SettingsUrl(String),
    SettingsCa(String),
    SettingsToken(String),
    SettingsSave,
    Connected {
        result: Result<Connection, PanelError>,
        saved: bool,
    },
}
