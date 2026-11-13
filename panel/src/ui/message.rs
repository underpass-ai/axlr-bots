use std::path::PathBuf;

use chrono::{DateTime, Utc};

use super::ModeChoice;
use super::table::{ColumnRef, List, RowRef};
use super::{BotChoice, BotView, Connection, Keypress, NodeChoice, ReaderKey, Screen, ThemeMode};
use crate::application::{
    BulkAction, BulkOutcome, ChatSent, DeletePreview, EventFeed, Fleet, PanelError, PanelSettings, SavedEdit,
    StoppedBot, SubscriptionChange, TaskDetail,
};
use crate::domain::{
    AutonomyLevel, Bot, BotId, CancelledTask, DecidedTask, DeletedBot, EnrollmentToken, ExecutionId, NodeId, Published,
    RevokedNode, SubmittedTask, TaskId, TaskStatus, ToolAccess, Topic,
};

/// Everything that happens in the panel: what the operator does, the
/// clock's ticks and the answers of the control plane.
#[derive(Debug, Clone)]
pub enum Message {
    Tick(DateTime<Utc>),
    Go(Screen),
    /// Esc or Enter, when no widget took it.
    Key(Keypress),
    /// A click, drag or key in a reader: it selects, it never edits.
    Read(ReaderKey, iced::widget::text_editor::Action),
    ToggleTheme,
    SetTheme(ThemeMode),
    DismissBanner,
    Copy(String),
    /// The person clicked a desktop notice about this place.
    NoticeClicked(Screen),
    /// The cursor or the selection moved in the answer's plain text.
    AnswerSelect(iced::widget::text_editor::Action),
    /// The answer as plain text to select (`true`), or as Markdown.
    AnswerSelecting(bool),

    FleetLoaded {
        generation: u64,
        result: Result<Fleet, PanelError>,
    },
    TaskLoaded(TaskId, Result<TaskDetail, PanelError>),
    /// What a read of the task's timeline brought, for its Activity.
    TaskFollowed(TaskId, Result<EventFeed, PanelError>),
    /// The whole activity tree of a task, as the control plane keeps it.
    ActivityLoaded(TaskId, Result<crate::domain::TaskActivity, PanelError>),
    /// What the stream of a task's activity tree brought.
    ActivityFollowed(TaskId, Result<crate::domain::ActivityBatch, PanelError>),
    /// Open or close the details of an Activity entry.
    ActivityPick(usize),
    /// The cursor or the selection moved in the model's words.
    WordsSelect(iced::widget::text_editor::Action),
    /// The model's words as text to select (`true`), or to read as they come.
    WordsSelecting(bool),
    /// The person scrolled a box of the running task's Activity: whether
    /// it now shows its end.
    ActivityScrolled(super::LiveStream, bool),
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
    ChatSent(Result<ChatSent, PanelError>),
    BotStopped(Result<StoppedBot, PanelError>),
    /// Update a bot to the version staged for it: ask first.
    AskUpdateBot(BotId),
    BotUpdated(Result<Bot, PanelError>),
    /// Give a bot an urgent order: open the form, type the order, send.
    /// It runs as an urgent task, which stops what the bot is doing and
    /// drops its queue.
    OrderOpen(BotId),
    OrderText(String),
    OrderSend,
    OrderClose,
    Ordered(Result<SubmittedTask, PanelError>),
    /// Change a bot's team: open the field, type, save.
    TeamEdit(BotId, String),
    TeamSave,
    TeamCancel,
    TeamSaved(Result<Bot, PanelError>),
    /// What a team's read of its timeline brought.
    TeamLoaded(String, Result<EventFeed, PanelError>),
    /// A team's topics; `None` from a control plane older than the route.
    TopicsLoaded(String, Result<Option<Vec<Topic>>, PanelError>),

    /// Edit a bot's mission and tools: open, change, save or cancel.
    EditOpen(BotId),
    EditClose,
    EditMission(String),
    /// What a listed tool may do, by its place in the list.
    EditAccess(usize, ToolAccess),
    EditRemove(usize),
    EditNewTool(String),
    EditNewAccess(ToolAccess),
    /// Add the tool typed, or the one a suggestion names.
    EditAdd(Option<String>),
    EditSave,
    EditSaved(Result<SavedEdit, PanelError>),
    /// Delete a bot: open the confirmation, read what it will do, type
    /// its id, delete.
    DeleteOpen(BotId),
    DeletePreviewed(BotId, Result<DeletePreview, PanelError>),
    DeleteTyped(String),
    DeleteClose,
    DeleteSend,
    BotDeleted(BotId, Result<DeletedBot, PanelError>),

    /// Make a bot listen to a topic: open the form, fill it, save.
    ListenOpen(BotId),
    ListenTopic(String),
    ListenMode(ModeChoice),
    ListenFrom(String),
    ListenSave,
    ListenClose,
    /// Stop a bot listening to a topic.
    Unsubscribe(BotId, String),
    /// Answer a bot's ask to listen.
    ApproveAsk(BotId, String),
    DenyAsk(BotId, String),
    SubscriptionsSaved(SubscriptionChange, Result<Bot, PanelError>),

    /// Publish an event on a team: open the form, fill it, send.
    PublishOpen(String),
    PublishName(String),
    PublishMessage(String),
    PublishTo(String),
    PublishSend,
    PublishClose,
    Published(Result<Published, PanelError>),
    /// Press one of the operator's shortcuts (its place in the list), and its answer.
    Shortcut(usize),
    ShortcutPublished(Result<Published, PanelError>),

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

    /// Save an execution's artifact, and where it went; `None` when the
    /// person cancelled.
    Download(ExecutionId, String),
    Downloaded(Result<Option<PathBuf>, PanelError>),

    ConfirmYes,
    ConfirmNo,

    SettingsUrl(String),
    SettingsCa(String),
    SettingsToken(String),
    /// Who the panel decides as: the actor a bot's approvers name.
    SettingsActor(String),
    SettingsSave,
    /// The New bot defaults typed: the model proposed, the models offered.
    SettingsModel(String),
    SettingsModels(String),
    /// Keep the look and the New bot defaults.
    SettingsSavePreferences,
    PreferencesSaved {
        result: Result<PanelSettings, PanelError>,
        /// Saved on the way, no word needed: the theme switch.
        quiet: bool,
    },
    /// The settings as JSON text: open, edit, apply, close.
    SettingsJsonOpen,
    SettingsJsonEdit(iced::widget::text_editor::Action),
    SettingsJsonApply,
    SettingsJsonClose,
    SettingsExport,
    SettingsExported(Result<Option<PathBuf>, PanelError>),
    SettingsImport,
    SettingsImported(Result<Option<PanelSettings>, PanelError>),
    /// Back to the install's values and the panel's own defaults.
    SettingsReset,
    Connected {
        result: Result<Connection, PanelError>,
        saved: bool,
    },

    /// The keyboard's modifiers as they change: Shift held makes a
    /// header press add its column to the order.
    Modifiers(iced::keyboard::Modifiers),
    /// A list's header pressed.
    SortBy(ColumnRef),
    /// A row's box ticked or not.
    Pick(RowRef, bool),
    /// The header's box: every row shown picked, or none.
    PickShown(List, bool),
    /// Let go of a list's picked rows.
    ClearPicks(List),
    /// An act from the bar over a list, on its picked rows: weigh it,
    /// then ask once.
    AskBulk(List, BulkAction),
    BulkDone(BulkOutcome),
}
