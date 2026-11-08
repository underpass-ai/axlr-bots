use chrono::{DateTime, Duration, Utc};

use super::{
    Answer, Banner, BotChoice, BotEditForm, BotView, Confirm, DeleteForm, Effect, IncidentWatch, Keypress, Launch,
    ListenForm, LiveStream, LiveTask, Message, MintForm, NodeChoice, OrderForm, PostmortemForm, PublishForm, Readers,
    RegisterForm, ResolveForm, Screen, Services, SettingsForm, SettingsJson, SubmitForm, TaskViewFilter, TeamWatch,
    ThemeMode, passages,
};
use crate::application::SubscriptionChange;
use crate::application::{EventFeed, Fleet, NewBotDefaults, PanelError, PanelSettings, TaskDetail};
use crate::domain::{Decision, Shortcut, TaskId, TaskStatus};
use crate::ui::format;
use crate::ui::table::Tables;

/// How long a refresh may stay unanswered before the next tick tries again.
const REFRESH_PATIENCE: i64 = 20;

/// The panel's whole state. `apply` turns a message into the new state
/// and the effects to run; it does no input or output itself, so tests
/// drive it message by message.
pub struct App {
    pub services: Services,
    pub mode: ThemeMode,
    pub screen: Screen,
    pub now: DateTime<Utc>,
    pub fleet: Option<Fleet>,
    /// The waiting tasks a notice already told the person about.
    pub noticed: std::collections::BTreeSet<TaskId>,
    /// The control plane's address, `host:port`, once connected.
    pub endpoint: Option<String>,
    /// Why the last refresh failed, until one succeeds.
    pub fleet_error: Option<PanelError>,
    pub last_refresh: Option<DateTime<Utc>>,
    refreshing_since: Option<DateTime<Utc>>,
    generation: u64,
    pub task: Option<TaskDetail>,
    /// The task's answer, parsed once per answer.
    pub answer: Option<Answer>,
    task_loading: bool,
    /// The task on screen as it is followed live: its Activity.
    pub live: Option<LiveTask>,
    pub bot: Option<BotView>,
    bot_loading: bool,
    pub mint: MintForm,
    pub register: RegisterForm,
    pub submit: SubmitForm,
    pub settings: SettingsForm,
    /// The settings as JSON text, when open to edit.
    pub settings_json: SettingsJson,
    /// The New bot defaults as saved: what a new form starts with.
    pub new_bot: NewBotDefaults,
    pub filter: TaskViewFilter,
    pub confirm: Option<Confirm>,
    pub banner: Option<Banner>,
    /// The task whose decision is on its way.
    pub deciding: Option<TaskId>,
    /// The message being written in the bot page's chat, and whether one
    /// is on its way.
    pub chat_draft: String,
    pub chat_busy: bool,
    /// A bot page's Advanced part is open.
    pub advanced: bool,
    /// A bot being switched on or paused.
    pub switching: bool,
    /// The teams' timelines and a bot's team being edited.
    pub watch: TeamWatch,
    /// Making a bot listen to a topic.
    pub listen: ListenForm,
    /// Editing a bot's mission and tools, and deleting a bot.
    pub edit: BotEditForm,
    pub delete: DeleteForm,
    /// Publishing an event on a team.
    pub publish: PublishForm,
    /// Giving a bot an urgent order.
    pub order: OrderForm,
    /// The operator's one-press buttons, read at start.
    pub shortcuts: Vec<Shortcut>,
    /// The incidents and the Resolve form.
    pub incidents: IncidentWatch,
    /// An artifact on its way to a file.
    pub downloading: bool,
    /// Where to go once connected.
    target: Screen,
    /// The passages of the screen on show a person can select a part of.
    pub readers: Readers,
    /// The list screens' tables: their order and their picked rows.
    pub tables: Tables,
}

impl App {
    /// The panel at start: connecting with saved settings, or on the
    /// settings screen the first time. `saved` says whether the control
    /// plane section was saved before.
    pub fn new(services: Services, launch: Launch, settings: &PanelSettings, saved: bool) -> (Self, Vec<Effect>) {
        let now = services.clock.now();
        let mut app = Self {
            services,
            mode: launch.theme.unwrap_or(settings.theme),
            screen: if saved { launch.screen.clone() } else { Screen::Settings },
            now,
            fleet: None,
            noticed: Default::default(),
            endpoint: None,
            fleet_error: None,
            last_refresh: None,
            refreshing_since: None,
            generation: 0,
            task: None,
            answer: None,
            task_loading: false,
            live: None,
            bot: None,
            bot_loading: false,
            mint: MintForm::default(),
            register: RegisterForm::default(),
            submit: SubmitForm::default(),
            settings: SettingsForm::from_settings(settings, saved),
            settings_json: SettingsJson::default(),
            new_bot: settings.new_bot.clone(),
            filter: TaskViewFilter::default(),
            confirm: None,
            banner: None,
            deciding: None,
            chat_draft: String::new(),
            chat_busy: false,
            advanced: false,
            switching: false,
            watch: TeamWatch::default(),
            listen: ListenForm::default(),
            edit: BotEditForm::default(),
            delete: DeleteForm::default(),
            publish: PublishForm::default(),
            order: OrderForm::default(),
            shortcuts: Vec::new(),
            incidents: IncidentWatch::default(),
            downloading: false,
            target: if launch.screen == Screen::Settings {
                Screen::Overview
            } else {
                launch.screen
            },
            readers: Readers::default(),
            tables: Tables::default(),
        };
        match app.services.shortcuts.load() {
            Ok(list) => app.shortcuts = list,
            Err(error) => app.banner = Some(Banner::error(&error, now)),
        }
        let effects = if saved {
            app.settings.busy = true;
            app.settings.trying = Some(app.settings.config());
            vec![Effect::Connect {
                config: app.settings.config(),
                save: false,
            }]
        } else {
            Vec::new()
        };
        (app, effects)
    }

    /// Everything the settings screen shows, as settings.
    pub fn current_settings(&self) -> PanelSettings {
        self.settings.settings(self.mode)
    }

    pub fn connected(&self) -> bool {
        self.services.control_plane.is_some()
    }

    /// The window's title: the control plane once connected, and how many
    /// tasks wait for a person.
    pub fn window_title(&self) -> String {
        let mut title = "AXLR Bots".to_string();
        if let Some(endpoint) = &self.endpoint {
            title = format!("{title} · {endpoint}");
        }
        match self.waiting_count() {
            0 => title,
            n => format!("{title} · {n} need you"),
        }
    }

    /// What waits for a person, from the last refresh: tasks waiting
    /// for a decision and bots' asks to listen to a topic.
    pub fn waiting_count(&self) -> u32 {
        self.fleet
            .as_ref()
            .map(|f| f.overview.tasks_in(TaskStatus::WaitingApproval))
            .unwrap_or(0)
            + self.pending_asks()
    }

    pub fn apply(&mut self, message: Message) -> Vec<Effect> {
        let effects = self.handle(message);
        self.readers.sync(passages::of(self));
        effects
    }

    fn handle(&mut self, message: Message) -> Vec<Effect> {
        match message {
            Message::Tick(now) => self.tick(now),
            Message::Go(screen) => self.go(screen),
            Message::Key(key) => self.key(key),
            Message::Read(key, action) => {
                self.readers.act(&key, action);
                Vec::new()
            }
            Message::ToggleTheme => {
                self.mode = self.mode.toggled();
                self.save_preferences(self.new_bot.clone(), true)
            }
            Message::SetTheme(mode) => {
                self.mode = mode;
                self.save_preferences(self.new_bot.clone(), true)
            }
            Message::DismissBanner => {
                self.banner = None;
                Vec::new()
            }
            Message::Copy(text) => {
                self.banner = Some(Banner::good("Copied", "", self.now));
                vec![Effect::Copy(text)]
            }
            Message::AnswerSelect(action) => {
                if let Some(answer) = self.answer.as_mut() {
                    answer.select(action);
                }
                Vec::new()
            }
            Message::AnswerSelecting(selecting) => {
                if let Some(answer) = self.answer.as_mut() {
                    answer.selecting = selecting;
                }
                Vec::new()
            }
            Message::FleetLoaded { generation, result } => {
                if generation != self.generation {
                    return Vec::new();
                }
                self.refreshing_since = None;
                match result {
                    Ok(fleet) => {
                        let mut effects = self.notice_waiting(&fleet, self.fleet.is_none());
                        self.fleet = Some(fleet);
                        self.drop_gone_picks();
                        self.fleet_error = None;
                        self.last_refresh = Some(self.now);
                        effects.extend(self.follow_teams());
                        effects
                    }
                    Err(error) => {
                        self.fleet_error = Some(error);
                        Vec::new()
                    }
                }
            }
            Message::TaskLoaded(id, result) => {
                self.task_loading = false;
                if self.screen != Screen::Task(id) {
                    return Vec::new();
                }
                match result {
                    Ok(detail) => {
                        let answer = detail.answer().map(str::to_string);
                        if answer.as_deref() != self.answer.as_ref().map(|a| a.source.as_str()) {
                            self.answer = answer.map(Answer::new);
                        }
                        self.task = Some(detail);
                        self.settle_live();
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                self.follow_task()
            }
            Message::TaskFollowed(id, result) => self.task_followed(id, result),
            Message::ActivityPick(index) => {
                if let Some(live) = self.live.as_mut() {
                    live.pick(index);
                }
                Vec::new()
            }
            Message::WordsSelect(action) => {
                if let Some(live) = self.live.as_mut() {
                    live.select(action);
                }
                Vec::new()
            }
            Message::WordsSelecting(selecting) => match self.live.as_mut() {
                Some(live) => {
                    live.selecting(selecting);
                    // Back to the formatted words: a new box, at its end
                    // while the person had not scrolled up in it.
                    if !selecting && live.follows(LiveStream::Words) {
                        vec![Effect::ShowEnd(LiveStream::Words)]
                    } else {
                        Vec::new()
                    }
                }
                None => Vec::new(),
            },
            Message::ActivityScrolled(stream, at_end) => {
                if let Some(live) = self.live.as_mut() {
                    live.scrolled(stream, at_end);
                }
                Vec::new()
            }
            Message::BotLoaded(id, result) => {
                self.bot_loading = false;
                if self.screen != Screen::Bot(id) {
                    return Vec::new();
                }
                match result {
                    Ok(view) => self.bot = Some(view),
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                Vec::new()
            }

            Message::MintOpen => {
                self.mint = MintForm::opened();
                Vec::new()
            }
            Message::MintClose => {
                self.mint = MintForm::default();
                Vec::new()
            }
            Message::MintNodeId(value) => {
                self.mint.node_id = value;
                Vec::new()
            }
            Message::MintLabels(value) => {
                self.mint.labels = value;
                Vec::new()
            }
            Message::MintTtl(value) => {
                self.mint.ttl = value;
                Vec::new()
            }
            Message::MintSubmit => {
                if self.mint.busy {
                    return Vec::new();
                }
                self.mint.busy = true;
                vec![Effect::Mint {
                    node_id: self.mint.node_id.clone(),
                    labels: self.mint.labels.clone(),
                    ttl: self.mint.ttl.clone(),
                }]
            }
            Message::Minted(result) => {
                self.mint.busy = false;
                match result {
                    Ok(token) => {
                        self.banner = Some(Banner::good("Token ready", format!("for {}", token.node_id), self.now));
                        self.mint.token = Some(token);
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                Vec::new()
            }
            Message::AskRevoke(id) => {
                self.confirm = Some(Confirm::Revoke(id));
                Vec::new()
            }
            Message::Revoked(result) => {
                match result {
                    Ok(revoked) => {
                        let failed = revoked.failed_tasks.len();
                        let detail = if failed == 0 {
                            "no task failed".to_string()
                        } else {
                            format!("{failed} tasks failed")
                        };
                        self.banner = Some(Banner::good(format!("{} revoked", revoked.node.id), detail, self.now));
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                self.refresh_now()
            }

            Message::RegisterOpen => {
                self.register = RegisterForm::opened(&self.new_bot);
                Vec::new()
            }
            Message::RegisterClose => {
                self.register = RegisterForm::default();
                Vec::new()
            }
            Message::RegisterName(value) => {
                self.register.form.name = value;
                Vec::new()
            }
            Message::RegisterMission(value) => {
                self.register.form.mission = value;
                Vec::new()
            }
            Message::RegisterLevel(level) => {
                self.register.form.level = level;
                Vec::new()
            }
            Message::RegisterTools(value) => {
                self.register.form.tools = value;
                Vec::new()
            }
            Message::RegisterModel(value) => {
                self.register.form.model = value;
                Vec::new()
            }
            Message::RegisterTeam(value) => {
                self.register.form.team = value;
                Vec::new()
            }
            Message::RegisterNode(choice) => {
                self.register.form.node = match choice {
                    NodeChoice::Any => String::new(),
                    NodeChoice::One(id) => id.to_string(),
                };
                Vec::new()
            }
            Message::RegisterAdvanced(open) => {
                self.register.advanced = open;
                Vec::new()
            }
            Message::RegisterSubmit => {
                if self.register.busy {
                    return Vec::new();
                }
                self.register.busy = true;
                vec![Effect::RegisterForm(self.register.form.clone())]
            }
            Message::PickFile => vec![Effect::PickFile],
            Message::FilePicked(result) => match result {
                Ok(Some(path)) => {
                    self.register.busy = true;
                    vec![Effect::RegisterFile(path)]
                }
                Ok(None) => Vec::new(),
                Err(error) => {
                    self.banner = Some(Banner::error(&error, self.now));
                    Vec::new()
                }
            },
            Message::Registered(result) => {
                self.register.busy = false;
                match result {
                    Ok(bot) => {
                        self.banner = Some(Banner::good(
                            format!("{} added", bot.id),
                            "Switch it on to give it tasks",
                            self.now,
                        ));
                        self.register = RegisterForm::default();
                        let mut effects = self.go(Screen::Bot(bot.id));
                        effects.extend(self.refresh_now());
                        effects
                    }
                    Err(error) => {
                        self.banner = Some(Banner::error(&error, self.now));
                        Vec::new()
                    }
                }
            }
            Message::Enable(id) => self.switch(Effect::Enable(id)),
            Message::Pause(id) => self.switch(Effect::Pause(id)),
            Message::Switched(result) => {
                self.switching = false;
                match result {
                    Ok(bot) => {
                        let word = if bot.status.accepts_tasks() {
                            "is on"
                        } else {
                            "is paused"
                        };
                        self.banner = Some(Banner::good(format!("{} {word}", bot.id), "", self.now));
                        self.replace_bot(bot);
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                self.refresh_now()
            }

            Message::ChatInput(value) => {
                self.chat_draft = value;
                Vec::new()
            }
            Message::ChatSend => {
                let Some(view) = self.bot.as_ref() else {
                    return Vec::new();
                };
                if self.chat_busy || self.chat_draft.trim().is_empty() {
                    return Vec::new();
                }
                self.chat_busy = true;
                vec![Effect::SendChat {
                    bot: view.bot.id.clone(),
                    message: self.chat_draft.clone(),
                    earlier: view.chat.clone(),
                    running: self.running_task(&view.bot),
                }]
            }
            Message::ChatSent(result) => {
                self.chat_busy = false;
                match result {
                    Ok(_) => self.chat_draft.clear(),
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                let mut effects = self.reload_screen();
                effects.extend(self.refresh_now());
                effects
            }
            Message::ShowAdvanced(open) => {
                self.advanced = open;
                Vec::new()
            }
            Message::SetLevel(id, level) => self.switch(Effect::SetLevel(id, level)),
            Message::Leveled(result) => {
                self.switching = false;
                match result {
                    Ok(bot) => {
                        self.banner = Some(Banner::good(
                            format!("{} is now {}", bot.id, bot.level.label()),
                            "From its next step",
                            self.now,
                        ));
                        self.replace_bot(bot);
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                let mut effects = self.reload_screen();
                effects.extend(self.refresh_now());
                effects
            }
            Message::OrderOpen(id) => self.order_open(id),
            Message::OrderText(value) => {
                self.order.text = value;
                Vec::new()
            }
            Message::OrderSend => self.order_send(),
            Message::OrderClose => {
                self.order = OrderForm::default();
                Vec::new()
            }
            Message::Ordered(result) => self.ordered(result),
            Message::TeamEdit(id, words) => self.team_edit(id, words),
            Message::TeamSave => self.team_save(),
            Message::TeamCancel => {
                self.watch.editing = None;
                Vec::new()
            }
            Message::TeamSaved(result) => self.team_saved(result),
            Message::TeamLoaded(name, result) => self.team_loaded(name, result),
            Message::TopicsLoaded(name, result) => self.topics_loaded(name, result),
            message @ (Message::EditOpen(_)
            | Message::EditClose
            | Message::EditMission(_)
            | Message::EditAccess(..)
            | Message::EditRemove(_)
            | Message::EditNewTool(_)
            | Message::EditNewAccess(_)
            | Message::EditAdd(_)
            | Message::EditSave
            | Message::EditSaved(_)
            | Message::DeleteOpen(_)
            | Message::DeletePreviewed(..)
            | Message::DeleteTyped(_)
            | Message::DeleteClose
            | Message::DeleteSend
            | Message::BotDeleted(..)) => self.bot_edit(message),
            Message::ListenOpen(bot) => {
                self.listen = ListenForm::opened(bot);
                Vec::new()
            }
            Message::ListenTopic(value) => {
                self.listen.topic = value;
                Vec::new()
            }
            Message::ListenMode(mode) => {
                self.listen.mode = mode;
                Vec::new()
            }
            Message::ListenFrom(value) => {
                self.listen.from = value;
                Vec::new()
            }
            Message::ListenSave => self.listen_save(),
            Message::ListenClose => {
                self.listen = ListenForm::default();
                Vec::new()
            }
            Message::Unsubscribe(bot, topic) => self.change_subscriptions(bot, SubscriptionChange::Remove(topic)),
            Message::ApproveAsk(bot, topic) => self.change_subscriptions(bot, SubscriptionChange::Approve(topic)),
            Message::DenyAsk(bot, topic) => self.change_subscriptions(bot, SubscriptionChange::Deny(topic)),
            Message::SubscriptionsSaved(change, result) => self.subscriptions_saved(change, result),
            Message::PublishOpen(team) => self.publish_open(team),
            Message::PublishName(value) => {
                self.publish.name = value;
                Vec::new()
            }
            Message::PublishMessage(value) => {
                self.publish.message = value;
                Vec::new()
            }
            Message::PublishTo(value) => {
                self.publish.to = value;
                Vec::new()
            }
            Message::PublishSend => self.publish_send(),
            Message::PublishClose => {
                self.publish = PublishForm::default();
                Vec::new()
            }
            Message::Published(result) => self.published(result),
            Message::Shortcut(index) => self.shortcut(index),
            Message::ShortcutPublished(result) => self.shortcut_published(result),
            Message::NoticeClicked(screen) => self.notice_clicked(screen),
            Message::AskStopBot(id) => {
                self.confirm = Some(Confirm::StopBot(id));
                Vec::new()
            }
            Message::BotStopped(result) => {
                self.switching = false;
                match result {
                    Ok(stopped) => {
                        let mut detail = match stopped.cancelled {
                            0 => "no open task".to_string(),
                            1 => "1 task cancelled".to_string(),
                            n => format!("{n} tasks cancelled"),
                        };
                        if stopped.not_cancelled > 0 {
                            detail = format!("{detail}, {} could not be", stopped.not_cancelled);
                        }
                        self.banner = Some(Banner::good(format!("{} stopped", stopped.bot.id), detail, self.now));
                        self.replace_bot(stopped.bot);
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                let mut effects = self.reload_screen();
                effects.extend(self.refresh_now());
                effects
            }

            Message::FilterStatus(status) => {
                self.filter.status = status;
                self.drop_gone_picks();
                if self.screen != Screen::Tasks {
                    return self.go(Screen::Tasks);
                }
                Vec::new()
            }
            Message::FilterBot(choice) => {
                self.filter.bot = match choice {
                    BotChoice::All => None,
                    BotChoice::One(id) => Some(id),
                };
                self.drop_gone_picks();
                Vec::new()
            }
            Message::SubmitOpen(bot) => {
                let bot = bot.or_else(|| self.filter.bot.clone());
                self.submit = SubmitForm::opened(bot, self.services.keys.next_key());
                if self.screen != Screen::Tasks {
                    return self.go(Screen::Tasks);
                }
                Vec::new()
            }
            Message::SubmitClose => {
                self.submit = SubmitForm::default();
                Vec::new()
            }
            Message::SubmitBot(choice) => {
                self.submit.bot = match choice {
                    BotChoice::All => None,
                    BotChoice::One(id) => Some(id),
                };
                Vec::new()
            }
            Message::SubmitObjective(value) => {
                self.submit.objective = value;
                Vec::new()
            }
            Message::SubmitUrgent(urgent) => {
                self.submit.urgent = urgent;
                Vec::new()
            }
            Message::SubmitSend => {
                let (Some(bot), Some(key)) = (self.submit.bot.clone(), self.submit.key.clone()) else {
                    return Vec::new();
                };
                if !self.submit.ready() {
                    return Vec::new();
                }
                self.submit.busy = true;
                vec![Effect::Submit {
                    bot,
                    objective: self.submit.objective.clone(),
                    key,
                    urgent: self.submit.urgent,
                }]
            }
            Message::Submitted(result) => {
                self.submit.busy = false;
                match result {
                    Ok(submitted) => {
                        let title = if submitted.created { "Task sent" } else { "Already sent" };
                        self.banner = Some(Banner::good(title, format!("to {}", submitted.task.bot_id), self.now));
                        self.submit = SubmitForm::default();
                        let mut effects = self.go(Screen::Task(submitted.task.id));
                        effects.extend(self.refresh_now());
                        effects
                    }
                    Err(error) => {
                        self.banner = Some(Banner::error(&error, self.now));
                        Vec::new()
                    }
                }
            }
            Message::AskCancel(id) => {
                self.confirm = Some(Confirm::Cancel(id));
                Vec::new()
            }
            Message::Cancelled(result) => {
                match result {
                    Ok(cancelled) => {
                        let title = if cancelled.changed {
                            "Task stopped"
                        } else {
                            "Already stopped"
                        };
                        self.banner = Some(Banner::good(title, cancelled.task.id.to_string(), self.now));
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                let mut effects = self.reload_screen();
                effects.extend(self.refresh_now());
                effects
            }
            Message::Approve(id) => self.decide(id, Decision::Approve),
            Message::AskDeny(id) => {
                self.confirm = Some(Confirm::Deny(id));
                Vec::new()
            }
            Message::Decided(result) => {
                self.deciding = None;
                match result {
                    Ok(decided) if decided.task.status == TaskStatus::WaitingApproval => {
                        // The first of two approvals: the call did not run,
                        // a second, different person decides it.
                        self.banner = Some(Banner::good(
                            "First approval recorded",
                            "Nothing ran: a second, different person decides",
                            self.now,
                        ));
                    }
                    Ok(decided) => {
                        let title = if decided.execution.decision == "deny" {
                            "Denied"
                        } else {
                            "Approved"
                        };
                        self.banner = Some(Banner::good(title, "The run goes on", self.now));
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                let mut effects = self.reload_screen();
                effects.extend(self.refresh_now());
                effects
            }

            Message::IncidentsLoaded(result) => self.incidents_loaded(result),
            Message::IncidentLoaded(id, result) => self.incident_loaded(id, result),
            Message::ResolveOpen => self.resolve_open(),
            Message::ResolveReason(value) => {
                self.incidents.resolve.reason = value;
                Vec::new()
            }
            Message::ResolveSend => self.resolve_send(),
            Message::ResolveClose => {
                self.incidents.resolve = ResolveForm::default();
                Vec::new()
            }
            Message::IncidentResolved(result) => self.incident_resolved(result),
            Message::MarkDiagnosis(verdict) => self.mark_diagnosis(verdict),
            Message::DiagnosisMarked(id, result) => self.diagnosis_marked(id, result),
            Message::PostmortemApprove => self.postmortem_approve(),
            Message::PostmortemBackOpen => {
                self.incidents.review = PostmortemForm::opened();
                Vec::new()
            }
            Message::PostmortemBackNote(value) => {
                self.incidents.review.note = value;
                Vec::new()
            }
            Message::PostmortemBackSend => self.postmortem_back_send(),
            Message::PostmortemBackClose => {
                self.incidents.review = PostmortemForm::default();
                Vec::new()
            }
            Message::PostmortemReviewed(result) => self.postmortem_reviewed(result),
            Message::Download(execution, name) => self.download(execution, name),
            Message::Downloaded(result) => self.downloaded(result),

            Message::ConfirmYes => match self.confirm.take() {
                Some(Confirm::Revoke(id)) => vec![Effect::Revoke(id)],
                Some(Confirm::Cancel(id)) => vec![Effect::Cancel(id)],
                Some(Confirm::Deny(id)) => self.decide(id, Decision::Deny),
                Some(Confirm::StopBot(id)) => {
                    self.switching = true;
                    vec![Effect::StopBot(id)]
                }
                Some(Confirm::Bulk(plan)) => self.run_bulk(plan),
                None => Vec::new(),
            },
            Message::ConfirmNo => {
                self.confirm = None;
                Vec::new()
            }

            Message::SettingsUrl(value) => {
                self.settings.api_url = value;
                Vec::new()
            }
            Message::SettingsCa(value) => {
                self.settings.ca_path = value;
                Vec::new()
            }
            Message::SettingsToken(value) => {
                self.settings.token_file = value;
                Vec::new()
            }
            Message::SettingsActor(value) => {
                self.settings.actor = value;
                Vec::new()
            }
            Message::SettingsSave => {
                if !self.can_connect() {
                    return Vec::new();
                }
                self.settings.busy = true;
                self.settings.trying = Some(self.settings.config());
                vec![Effect::Connect {
                    config: self.settings.config(),
                    save: true,
                }]
            }
            Message::SettingsModel(value) => {
                self.settings.default_model = value;
                Vec::new()
            }
            Message::SettingsModels(value) => {
                self.settings.models = value;
                Vec::new()
            }
            Message::SettingsSavePreferences => self.save_preferences(self.settings.new_bot(), false),
            Message::PreferencesSaved { result, quiet } => self.preferences_saved(result, quiet),
            Message::SettingsJsonOpen => {
                self.settings_json.open_with(&self.current_settings().to_json());
                Vec::new()
            }
            Message::SettingsJsonEdit(action) => {
                self.settings_json.content.perform(action);
                self.settings_json.error = None;
                Vec::new()
            }
            Message::SettingsJsonApply => match PanelSettings::from_json(&self.settings_json.text()) {
                Ok(settings) => {
                    self.settings_json.close();
                    self.take_settings(settings, "Applied")
                }
                Err(error) => {
                    self.settings_json.error = Some(match error {
                        PanelError::Config(words) => words,
                        other => other.to_string(),
                    });
                    Vec::new()
                }
            },
            Message::SettingsJsonClose => {
                self.settings_json.close();
                Vec::new()
            }
            Message::SettingsExport => vec![Effect::ExportSettings(self.current_settings())],
            Message::SettingsExported(result) => {
                match result {
                    Ok(Some(path)) => {
                        self.banner = Some(Banner::good("Settings exported", path.display().to_string(), self.now));
                    }
                    Ok(None) => {}
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
                }
                Vec::new()
            }
            Message::SettingsImport => vec![Effect::ImportSettings],
            Message::SettingsImported(result) => match result {
                Ok(Some(settings)) => self.take_settings(settings, "Settings imported"),
                Ok(None) => Vec::new(),
                Err(error) => {
                    self.banner = Some(Banner::error(&error, self.now));
                    Vec::new()
                }
            },
            Message::SettingsReset => {
                let (control_plane, new_bot) = self.services.connect.defaults();
                let settings = PanelSettings {
                    control_plane: Some(control_plane),
                    theme: ThemeMode::default(),
                    new_bot,
                };
                self.take_settings(settings, "Back to the defaults")
            }
            Message::Connected { result, saved } => {
                self.settings.busy = false;
                let tried = self.settings.trying.take();
                match result {
                    Ok(connection) => {
                        self.services.control_plane = Some(connection.0);
                        self.settings.connected = tried;
                        self.endpoint = Some(format::endpoint(
                            &self
                                .settings
                                .connected
                                .as_ref()
                                .map_or(String::new(), |c| c.api_url.clone()),
                        ));
                        self.fleet_error = None;
                        if saved {
                            self.settings.saved = true;
                            self.banner = Some(Banner::good("Connected", "Settings saved", self.now));
                        }
                        let mut effects = if self.screen == Screen::Settings && saved {
                            self.go(self.target.clone())
                        } else {
                            self.reload_screen()
                        };
                        effects.extend(self.refresh_now());
                        effects
                    }
                    Err(error) => {
                        self.fleet_error = Some(error.clone());
                        self.banner = Some(Banner::error(&error, self.now));
                        self.screen = Screen::Settings;
                        Vec::new()
                    }
                }
            }

            Message::Modifiers(modifiers) => {
                self.tables.shift = modifiers.shift();
                Vec::new()
            }
            Message::SortBy(column) => self.sort_by(column),
            Message::Pick(row, picked) => self.pick(row, picked),
            Message::PickShown(list, picked) => self.pick_shown(list, picked),
            Message::ClearPicks(list) => self.clear_picks(list),
            Message::AskBulk(list, action) => self.ask_bulk(list, action),
            Message::BulkDone(outcome) => self.bulk_done(outcome),
        }
    }

    /// Connect has something to do: the values typed are not the ones
    /// connected, or the connection broke (a control plane restarted, a
    /// token rotated) and these values are worth trying again.
    pub fn can_connect(&self) -> bool {
        self.settings.can_connect() || (!self.settings.busy && self.fleet_error.is_some())
    }

    /// Keeps the look as it is and the New bot defaults given: the saved
    /// ones on a theme switch, so a half-typed field is not committed;
    /// the typed ones on Save.
    fn save_preferences(&mut self, new_bot: NewBotDefaults, quiet: bool) -> Vec<Effect> {
        let settings = PanelSettings {
            new_bot,
            ..self.current_settings()
        };
        vec![Effect::SavePreferences { settings, quiet }]
    }

    fn preferences_saved(&mut self, result: Result<PanelSettings, PanelError>, quiet: bool) -> Vec<Effect> {
        match result {
            Ok(kept) => {
                self.new_bot = kept.new_bot;
                if !quiet {
                    self.banner = Some(Banner::good("Settings saved", "The New bot defaults", self.now));
                }
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        Vec::new()
    }

    /// Settings that arrived whole (the JSON applied, a file imported, the
    /// defaults): the screen takes them, the look and the New bot
    /// defaults are kept at once, and the control plane values wait for
    /// Connect when they differ from the connection.
    fn take_settings(&mut self, settings: PanelSettings, title: &str) -> Vec<Effect> {
        self.settings.take(&settings);
        self.mode = settings.theme;
        let words = if self.settings.changed() || (!self.connected() && !self.settings.busy) {
            "Press Connect to try the control plane values"
        } else {
            "Look and New bot defaults kept"
        };
        self.banner = Some(Banner::good(title, words, self.now));
        self.save_preferences(settings.new_bot, true)
    }

    fn tick(&mut self, now: DateTime<Utc>) -> Vec<Effect> {
        self.now = now;
        if self.banner.as_ref().is_some_and(|b| b.expired(now)) {
            self.banner = None;
        }
        if !self.connected() {
            return Vec::new();
        }
        let stale = self
            .refreshing_since
            .is_none_or(|since| now - since > Duration::seconds(REFRESH_PATIENCE));
        let mut effects = if stale { self.refresh_now() } else { Vec::new() };
        effects.extend(self.reload_screen());
        effects
    }

    /// A refresh, now: a new generation so an older answer is dropped.
    pub(super) fn refresh_now(&mut self) -> Vec<Effect> {
        if !self.connected() {
            return Vec::new();
        }
        self.generation += 1;
        self.refreshing_since = Some(self.now);
        vec![Effect::Refresh {
            generation: self.generation,
        }]
    }

    /// Reads again what the screen shows beside the fleet: a task's detail
    /// or a bot's events, unless a read is on its way.
    pub(super) fn reload_screen(&mut self) -> Vec<Effect> {
        if !self.connected() {
            return Vec::new();
        }
        match &self.screen {
            Screen::Task(id) => {
                let mut effects = Vec::new();
                if !self.task_loading {
                    self.task_loading = true;
                    effects.push(Effect::LoadTask(id.clone()));
                }
                effects.extend(self.follow_task());
                effects
            }
            Screen::Bot(id) if !self.bot_loading => {
                self.bot_loading = true;
                let feed = self
                    .bot
                    .as_ref()
                    .filter(|v| &v.bot.id == id)
                    .map(|v| v.feed.clone())
                    .unwrap_or_default();
                vec![Effect::FollowBot(id.clone(), feed)]
            }
            Screen::Team(name) if !self.watch.loading_topics.contains(name) => {
                self.watch.loading_topics.insert(name.clone());
                vec![Effect::LoadTopics(name.clone())]
            }
            Screen::Incidents if !self.incidents.list_loading => {
                self.incidents.list_loading = true;
                vec![Effect::LoadIncidents]
            }
            Screen::Incident(id) if !self.incidents.detail_loading => {
                self.incidents.detail_loading = true;
                vec![Effect::LoadIncident(id.clone())]
            }
            _ => Vec::new(),
        }
    }

    pub(super) fn go(&mut self, screen: Screen) -> Vec<Effect> {
        if let Screen::Task(id) = &screen
            && self.task.as_ref().is_some_and(|t| &t.task.id != id)
        {
            self.task = None;
            self.answer = None;
        }
        if let Screen::Task(id) = &screen
            && self.live.as_ref().is_some_and(|l| &l.id != id)
        {
            self.live = None;
        }
        if let Screen::Bot(id) = &screen
            && self.bot.as_ref().is_some_and(|b| &b.bot.id != id)
        {
            self.bot = None;
        }
        if let Screen::Incident(id) = &screen
            && self.incidents.detail.as_ref().is_some_and(|i| &i.id != id)
        {
            self.incidents.detail = None;
        }
        // Another screen opens at its top, not where the last one was
        // scrolled to; a refresh of the same one keeps its place.
        let mut effects = Vec::new();
        if screen != self.screen {
            effects.push(Effect::ScrollTop);
            self.advanced = false;
            self.chat_draft.clear();
            self.watch.editing = None;
            self.listen = ListenForm::default();
            self.edit = BotEditForm::default();
            self.delete = DeleteForm::default();
            self.publish = PublishForm::default();
            self.order = OrderForm::default();
            self.task_loading = false;
            self.bot_loading = false;
            self.incidents.list_loading = false;
            self.incidents.detail_loading = false;
            self.incidents.review = PostmortemForm::default();
            self.incidents.resolve = ResolveForm::default();
            // The picked rows belong to the list left; its order stays.
            self.tables.clear_all();
        }
        self.screen = screen;
        self.confirm = None;
        if self.screen == Screen::Settings && !self.settings.busy {
            self.settings.saved = self.settings.saved || self.connected();
        }
        effects.extend(self.reload_screen());
        effects
    }

    /// Esc keeps what a dialog would do, closes the new task's form or
    /// a bot's Edit or Delete card, or lets go of the rows picked in a
    /// list; Enter does what the dialog asks. Other screens ignore them.
    fn key(&mut self, key: Keypress) -> Vec<Effect> {
        match key {
            Keypress::Escape if self.confirm.is_some() => self.handle(Message::ConfirmNo),
            Keypress::Enter if self.confirm.is_some() => self.handle(Message::ConfirmYes),
            Keypress::Escape if self.submit.open && !self.submit.busy => self.handle(Message::SubmitClose),
            Keypress::Escape if self.delete.bot.is_some() && !self.delete.busy => self.handle(Message::DeleteClose),
            Keypress::Escape if self.edit.bot.is_some() && !self.edit.busy => self.handle(Message::EditClose),
            Keypress::Escape => match self.list_on_screen() {
                Some(list) if self.tables.picked(list) > 0 => self.clear_picks(list),
                _ => Vec::new(),
            },
            _ => Vec::new(),
        }
    }

    /// The task a bot runs now, as the newest read of the fleet and its
    /// events say.
    pub fn running_task(&self, bot: &crate::domain::Bot) -> Option<TaskId> {
        let tasks = self.fleet.as_ref().map(|f| f.tasks.as_slice()).unwrap_or_default();
        let events = self.feed().map(|f| f.events.as_slice()).unwrap_or_default();
        match crate::application::BotNow::of(bot, tasks, events) {
            crate::application::BotNow::Working { task, .. } => Some(task.id),
            _ => None,
        }
    }

    /// The bot on screen, as the control plane now has it.
    pub(super) fn replace_bot(&mut self, bot: crate::domain::Bot) {
        if let Some(view) = self.bot.as_mut().filter(|v| v.bot.id == bot.id) {
            view.bot = bot;
        }
    }

    pub(super) fn switch(&mut self, effect: Effect) -> Vec<Effect> {
        if self.switching {
            return Vec::new();
        }
        self.switching = true;
        vec![effect]
    }

    fn decide(&mut self, id: TaskId, decision: Decision) -> Vec<Effect> {
        if self.deciding.is_some() {
            return Vec::new();
        }
        self.deciding = Some(id.clone());
        // The call on screen, so the control plane refuses the decision if
        // the task has since moved on to another call.
        let call_id = self
            .task
            .as_ref()
            .filter(|d| d.task.id == id)
            .and_then(|d| d.pending_call())
            .or_else(|| self.fleet.as_ref().and_then(|f| f.call_of(&id)))
            .map(|c| c.call_id.clone())
            .or_else(|| {
                // Decided from the incident's page: the call it says its
                // current task waits on.
                self.incidents
                    .detail
                    .as_ref()
                    .and_then(|i| i.decision())
                    .filter(|(task, call)| *task == &id && !call.is_empty())
                    .map(|(_, call)| call.to_string())
            });
        vec![Effect::Decide(id, decision, call_id)]
    }

    /// The followed bot's feed, if one is on screen.
    pub fn feed(&self) -> Option<&EventFeed> {
        self.bot.as_ref().map(|b| &b.feed)
    }
}
