use std::path::PathBuf;

use chrono::{DateTime, Utc};

use super::{BotChoice, BotView, Connection, NodeChoice, Screen, ThemeMode};
use crate::application::{EventFeed, Fleet, PanelError, StoppedBot, TaskDetail};
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
    SetTheme(ThemeMode),
    DismissBanner,
    Copy(String),
    /// The cursor or the selection moved in the answer's plain text.
    AnswerSelect(iced::widget::text_editor::Action),
    /// The answer as plain text to select (`true`), or as Markdown.
    AnswerSelecting(bool),

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
    /// Show or hide the New bot form's Advanced part.
    RegisterAdvanced(bool),
    RegisterTeam(String),
    RegisterSubmit,
    PickFile,
    FilePicked(Result<Option<PathBuf>, PanelError>),
    Registered(Result<Bot, PanelError>),
    Enable(BotId),
    Pause(BotId),
    Switched(Result<Bot, PanelError>),
    /// Open or close a bot page's Advanced part.
    ShowAdvanced(bool),
    /// Set how much a bot may do alone.
    SetLevel(BotId, AutonomyLevel),
    Leveled(Result<Bot, PanelError>),
    AskStopBot(BotId),
    /// The message being written in a bot's chat.
    ChatInput(String),
    ChatSend,
    ChatSent(Result<SubmittedTask, PanelError>),
    BotStopped(Result<StoppedBot, PanelError>),
    /// Bring a vehicle's bot home at once: an urgent task.
    Recall(BotId),
    Recalled(Result<SubmittedTask, PanelError>),
    /// Change a bot's team: open the field, type, save.
    TeamEdit(BotId, String),
    TeamSave,
    TeamCancel,
    TeamSaved(Result<Bot, PanelError>),
    /// What a team's read of its timeline brought.
    TeamLoaded(String, Result<EventFeed, PanelError>),

    FilterStatus(Option<TaskStatus>),
    FilterBot(BotChoice),
    SubmitOpen(Option<BotId>),
    SubmitClose,
    SubmitBot(BotChoice),
    SubmitObjective(String),
    SubmitUrgent(bool),
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
