use chrono::{DateTime, Duration, Utc};

use super::{
    Answer, Banner, BotChoice, BotView, Confirm, Effect, Launch, Message, MintForm, NodeChoice, RegisterForm, Screen,
    Services, SettingsForm, SubmitForm, TaskViewFilter, TeamWatch, ThemeMode,
};
use crate::application::{EventFeed, Fleet, PanelConfig, PanelError, TaskDetail};
use crate::domain::{Decision, TaskId, TaskStatus};
use crate::ui::format;

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
    pub bot: Option<BotView>,
    bot_loading: bool,
    pub mint: MintForm,
    pub register: RegisterForm,
    pub submit: SubmitForm,
    pub settings: SettingsForm,
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
    /// Where to go once connected.
    target: Screen,
}

impl App {
    /// The panel at start: connecting with saved settings, or on the
    /// settings screen the first time.
    pub fn new(services: Services, launch: Launch, config: &PanelConfig, saved: bool) -> (Self, Vec<Effect>) {
        let now = services.clock.now();
        let mut app = Self {
            services,
            mode: launch.theme,
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
            bot: None,
            bot_loading: false,
            mint: MintForm::default(),
            register: RegisterForm::default(),
            submit: SubmitForm::default(),
            settings: SettingsForm::from_config(config, saved),
            filter: TaskViewFilter::default(),
            confirm: None,
            banner: None,
            deciding: None,
            chat_draft: String::new(),
            chat_busy: false,
            advanced: false,
            switching: false,
            watch: TeamWatch::default(),
            target: if launch.screen == Screen::Settings {
                Screen::Overview
            } else {
                launch.screen
            },
        };
        let effects = if saved {
            app.settings.busy = true;
            vec![Effect::Connect {
                config: config.clone(),
                save: false,
            }]
        } else {
            Vec::new()
        };
        (app, effects)
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

    /// Tasks waiting for a person, from the last refresh.
    pub fn waiting_count(&self) -> u32 {
        self.fleet
            .as_ref()
            .map(|f| f.overview.tasks_in(TaskStatus::WaitingApproval))
            .unwrap_or(0)
    }

    pub fn apply(&mut self, message: Message) -> Vec<Effect> {
        match message {
            Message::Tick(now) => self.tick(now),
            Message::Go(screen) => self.go(screen),
            Message::ToggleTheme => {
                self.mode = self.mode.toggled();
                Vec::new()
            }
            Message::SetTheme(mode) => {
                self.mode = mode;
                Vec::new()
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
                    }
                    Err(error) => self.banner = Some(Banner::error(&error, self.now)),
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
                self.register = RegisterForm::opened();
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
            Message::Recall(id) => self.recall(id),
            Message::Recalled(result) => self.recalled(result),
            Message::TeamEdit(id, words) => self.team_edit(id, words),
            Message::TeamSave => self.team_save(),
            Message::TeamCancel => {
                self.watch.editing = None;
                Vec::new()
            }
            Message::TeamSaved(result) => self.team_saved(result),
            Message::TeamLoaded(name, result) => self.team_loaded(name, result),
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

            Message::ConfirmYes => match self.confirm.take() {
                Some(Confirm::Revoke(id)) => vec![Effect::Revoke(id)],
                Some(Confirm::Cancel(id)) => vec![Effect::Cancel(id)],
                Some(Confirm::Deny(id)) => self.decide(id, Decision::Deny),
                Some(Confirm::StopBot(id)) => {
                    self.switching = true;
                    vec![Effect::StopBot(id)]
                }
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
            Message::SettingsSave => {
                if self.settings.busy {
                    return Vec::new();
                }
                self.settings.busy = true;
                vec![Effect::Connect {
                    config: self.settings.config(),
                    save: true,
                }]
            }
            Message::Connected { result, saved } => {
                self.settings.busy = false;
                match result {
                    Ok(connection) => {
                        self.services.control_plane = Some(connection.0);
                        self.endpoint = Some(format::endpoint(&self.settings.api_url));
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
        }
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
            Screen::Task(id) if !self.task_loading => {
                self.task_loading = true;
                vec![Effect::LoadTask(id.clone())]
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
        if let Screen::Bot(id) = &screen
            && self.bot.as_ref().is_some_and(|b| &b.bot.id != id)
        {
            self.bot = None;
        }
        if screen != self.screen {
            self.advanced = false;
            self.chat_draft.clear();
            self.watch.editing = None;
            self.task_loading = false;
            self.bot_loading = false;
        }
        self.screen = screen;
        self.confirm = None;
        if self.screen == Screen::Settings && !self.settings.busy {
            self.settings.saved = self.settings.saved || self.connected();
        }
        self.reload_screen()
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
        vec![Effect::Decide(id, decision)]
    }

    /// The followed bot's feed, if one is on screen.
    pub fn feed(&self) -> Option<&EventFeed> {
        self.bot.as_ref().map(|b| &b.feed)
    }
}
