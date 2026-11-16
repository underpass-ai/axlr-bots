//! A bot in the inspector, in plain words: Pause, Stop, Edit, Delete and
//! Update in its header; what waits for a person, what it does now with
//! an urgent order, its latest work and its chat on Now; then its work,
//! its mission and team, its rules and tools, what it listens to and
//! emits with its events.

use iced::widget::text::{LineHeight, Wrapping};
use iced::widget::{Space, column, container, mouse_area, pick_list, row, text, text_input};
use iced::{Alignment, Length, Padding, mouse};

use super::super::levels;
use super::super::widgets::{
    El, SMALL, Voice, action, bold, chip, copy_button, empty, fact, icon, mark, muted, pill, reader, section_title,
};
use super::part;
use crate::application::{BotNow, ChatLine};
use crate::domain::{Bot, BotId, BotStatus, TaskId};
use crate::ui::format;
use crate::ui::inspector::{BotTab, Tab};
use crate::ui::styles::{self, Kind};
use crate::ui::{App, BotView, Icon, Message, ModeChoice, ReaderKey, Screen, Tokens, Tone, passages};

/// The bot inspected, `width` wide: its status, team and place with its
/// acts, its name, and its tabs. Edit and Delete, once asked for, come
/// first in the tab's body.
pub fn view<'a>(app: &'a App, id: &'a BotId, width: f32, tokens: Tokens) -> El<'a> {
    let Some(view) = app.bot.as_ref().filter(|v| &v.bot.id == id) else {
        return column![
            super::head(app, Vec::new(), Vec::new(), id.as_str(), None, width, tokens),
            empty(Icon::Hourglass, "Loading the bot…", tokens)
        ]
        .into();
    };
    let bot = &view.bot;
    let tab = app.tabs.bot;
    let mut pane = column![
        header(app, bot, width, tokens),
        super::tabs(Tab::Bot(tab), |t| count(app, bot, t), tokens)
    ];
    let mut top = column![].spacing(14);
    if let Some(card) = super::super::bot_delete::card(app, bot, tokens) {
        top = top.push(card);
    }
    if let Some(card) = super::super::bot_edit::card(app, bot, tokens) {
        top = top.push(card);
    }
    let content: El<'a> = match tab {
        BotTab::Now => now_tab(app, view, tokens),
        BotTab::Work => work(app, bot, tokens),
        BotTab::Mission => mission(app, bot, tokens),
        BotTab::Rules => advanced(app, bot, tokens),
        BotTab::Listens => listens_and_emits(app, bot, tokens),
    };
    pane = pane.push(super::body(column![top, content].spacing(16)));
    if tab == BotTab::Now {
        pane = pane.push(chat_foot(app, view, tokens));
    }
    pane.into()
}

/// The number beside a tab: its tasks, its tools, its topics.
fn count(app: &App, bot: &Bot, tab: Tab) -> Option<usize> {
    match tab {
        Tab::Bot(BotTab::Work) => Some(mine(app, bot).count()),
        Tab::Bot(BotTab::Rules) => Some(bot.allowed_tools.len()),
        Tab::Bot(BotTab::Listens) => {
            Some(bot.subscriptions.iter().filter(|s| !s.is_pending()).count() + emits(app).len())
        }
        _ => None,
    }
}

/// Its status, team and place, its version, and its acts: Update to a
/// staged version, Pause or Turn on, Stop, Edit, Delete.
fn header<'a>(app: &'a App, bot: &'a Bot, width: f32, tokens: Tokens) -> El<'a> {
    let team = if bot.groups.is_empty() {
        "no team".to_string()
    } else {
        bot.groups.join(", ")
    };
    let word = if bot.status == BotStatus::Enabled {
        "On"
    } else {
        bot.status.name()
    };
    let mut first = vec![
        pill(Tone::of_bot(bot.status), word, tokens),
        muted(format!("{team} · {}", bot.placement.describe()), tokens)
            .wrapping(Wrapping::None)
            .into(),
    ];
    // Its definition's version, and the newer one ready to apply: its
    // mark says from and to, the button asks before it updates.
    let update = bot.update();
    if let Some(version) = bot.version_label() {
        first.push(chip(version, tokens));
    }
    if let Some(update) = &update {
        first.push(mark(Icon::Update, tokens.accent, update.tip(), tokens));
    }
    let mut acts = Vec::new();
    if let Some(update) = &update {
        acts.push(action(
            Kind::Primary,
            Icon::Update,
            update.act(),
            (!app.switching).then(|| Message::AskUpdateBot(bot.id.clone())),
            tokens,
        ));
    }
    if bot.status.can_pause() {
        acts.push(action(
            Kind::Secondary,
            Icon::Pause,
            "Pause",
            (!app.switching).then(|| Message::Pause(bot.id.clone())),
            tokens,
        ));
    } else if bot.status.can_enable() {
        acts.push(action(
            Kind::Good,
            Icon::Play,
            "Turn on",
            (!app.switching).then(|| Message::Enable(bot.id.clone())),
            tokens,
        ));
    }
    acts.push(action(
        Kind::Danger,
        Icon::Stop,
        "Stop",
        (!app.switching).then(|| Message::AskStopBot(bot.id.clone())),
        tokens,
    ));
    acts.push(action(
        Kind::Secondary,
        Icon::Wrench,
        "Edit",
        (!app.edit.is_open_for(&bot.id)).then(|| Message::EditOpen(bot.id.clone())),
        tokens,
    ));
    acts.push(action(
        Kind::Secondary,
        Icon::Ban,
        "Delete",
        (!app.delete.is_open_for(&bot.id)).then(|| Message::DeleteOpen(bot.id.clone())),
        tokens,
    ));
    super::head(app, first, acts, bot.id.as_str(), None, width, tokens)
}

/// Now: the calls and asks that wait for a person, what it does right
/// now with the urgent order, its latest work, how it starts and its
/// chat.
fn now_tab<'a>(app: &'a App, view: &'a BotView, tokens: Tokens) -> El<'a> {
    let bot = &view.bot;
    let mut page = column![].spacing(16);
    if let Some(asking) = asking(app, &bot.id, tokens) {
        page = page.push(asking);
    }
    for card in asks(app, bot, tokens) {
        page = page.push(card);
    }
    page = page.push(part("Right now", now_and_order(app, bot, tokens), tokens));
    let latest: Vec<_> = mine(app, bot).take(10).collect();
    if !latest.is_empty() {
        let mut list = column![];
        for task in latest {
            list = list.push(work_line(app, task, tokens));
        }
        page = page.push(part("Latest work", list, tokens));
    }
    let starts = bot.triggers.iter().map(|trigger| {
        let schedule = trigger.schedule();
        if schedule.is_empty() {
            format!("{} {}", trigger.trigger_type.label(), trigger.display_name())
        } else {
            format!(
                "{} {} · {schedule}",
                trigger.trigger_type.label(),
                trigger.display_name()
            )
        }
    });
    page = page.push(part("Starts by", super::tags(starts, tokens), tokens));
    page.push(part("Talk to it", chat(app, view, tokens), tokens)).into()
}

/// The bot's newest tasks. A task older than the bot is a predecessor's
/// that had its id: a bot deleted and registered again starts afresh.
fn mine<'a>(app: &'a App, bot: &'a Bot) -> impl Iterator<Item = &'a crate::domain::Task> + 'a {
    app.fleet
        .iter()
        .flat_map(|f| f.tasks.iter())
        .filter(move |t| t.bot_id == bot.id && t.created_at >= bot.created_at)
}

/// A task of the bot on one line: its status, what it is about and its
/// age; a press opens it.
fn work_line<'a>(app: &'a App, task: &'a crate::domain::Task, tokens: Tokens) -> El<'a> {
    let title = crate::ui::find::TaskWhat::of(task).title().to_string();
    // The title cut with an ellipsis to the characters its room holds,
    // so a long one never stops mid-word at the edge.
    let fitted = iced::widget::responsive(move |room| {
        let fits = ((room.width / (0.52 * SMALL)).floor() as usize).max(4);
        text(super::super::grid::shorten(&title, fits))
            .size(SMALL)
            .wrapping(Wrapping::None)
            .into()
    })
    .height(Length::Shrink);
    super::mini(
        row![
            container(super::super::status::task(task.status, tokens)).width(96),
            container(fitted).width(Length::Fill).clip(true),
            container(muted(format::age(task.created_at, app.now), tokens))
                .width(40)
                .align_x(Alignment::End),
        ]
        .spacing(10)
        .align_y(Alignment::Center)
        .into(),
        Some(Message::Go(Screen::Task(task.id.clone()))),
        tokens,
    )
}

/// Work: every task of the bot among the newest, and a new one.
fn work<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let new_task = action(
        Kind::Secondary,
        Icon::Plus,
        "New task",
        bot.status
            .accepts_tasks()
            .then(|| Message::SubmitOpen(Some(bot.id.clone()))),
        tokens,
    );
    let mut list = column![row![new_task]].spacing(8);
    let mut lines = column![];
    let mut any = false;
    for task in mine(app, bot).take(60) {
        any = true;
        lines = lines.push(work_line(app, task, tokens));
    }
    if !any {
        return list.push(empty(Icon::Tasks, "No task among the newest", tokens)).into();
    }
    list = list.push(lines);
    list.into()
}

/// Mission: what it does, in its own words to select, and its team.
fn mission<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    column![
        part(
            "What it does",
            column![
                reader(
                    &app.readers,
                    ReaderKey::BotMission,
                    bot.mission.clone(),
                    Voice::Body,
                    tokens
                ),
                copy_button("Copy mission", bot.mission.clone(), tokens)
            ]
            .spacing(8),
            tokens
        ),
        team(app, bot, tokens),
    ]
    .spacing(16)
    .into()
}

/// Listens & emits: the topics it listens to with the form for one
/// more, the names it emitted, and its events.
fn listens_and_emits<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let emitted = emits(app);
    let emits: El<'a> = if emitted.is_empty() {
        muted("Nothing among its newest events", tokens).into()
    } else {
        super::tags(emitted, tokens)
    };
    let mut events_part = column![].spacing(8);
    for tool in event_tools(app, tokens) {
        events_part = events_part.push(tool);
    }
    events_part = events_part.push(events(app, tokens));
    column![
        listens(app, bot, tokens),
        part("Emits", emits, tokens),
        part("Events", events_part, tokens),
    ]
    .spacing(16)
    .into()
}

/// The names of the events the bot emitted, as its newest events say,
/// each once.
fn emits(app: &App) -> Vec<String> {
    let mut names: Vec<String> = Vec::new();
    for event in app.feed().map(|f| f.events.as_slice()).unwrap_or_default() {
        if event.event_type == "bot.event.emitted"
            && let Some(name) = event.payload_text("name")
            && !names.contains(&name)
        {
            names.push(name);
        }
    }
    names
}

/// The foot of Now: a message to the bot, which runs as a task or goes
/// to its running task, and the urgent order.
fn chat_foot<'a>(app: &'a App, view: &'a BotView, tokens: Tokens) -> El<'a> {
    let ready = !app.chat_busy && !app.chat_draft.trim().is_empty() && view.bot.status.accepts_tasks();
    let input = text_input(
        &format!("Message {}… each message runs as a task", view.bot.id),
        &app.chat_draft,
    )
    .on_input(Message::ChatInput)
    .on_submit_maybe(ready.then_some(Message::ChatSend))
    .padding(8)
    .size(SMALL)
    .style(styles::field(tokens));
    column![
        container(Space::new().width(Length::Fill).height(1)).style(styles::rule(tokens)),
        container(
            row![
                input,
                action(
                    Kind::Primary,
                    Icon::Play,
                    if app.chat_busy { "Sending…" } else { "Send" },
                    ready.then_some(Message::ChatSend),
                    tokens
                ),
                super::super::order::button(app, &view.bot, tokens),
            ]
            .spacing(8)
            .align_y(Alignment::Center)
        )
        .padding(Padding::from([10, 14]))
    ]
    .into()
}

/// The bot's chat in time order: messages that ran as tasks with their
/// answers, and messages to a running task with what its run said back;
/// a box to write the next one.
fn chat<'a>(app: &'a App, view: &'a BotView, tokens: Tokens) -> El<'a> {
    let mut list = column![].spacing(8);
    let running = app.running_task(&view.bot);
    let hint = match running.as_ref() {
        Some(_) => "It is working now: your message goes to its run, and it answers between its steps.",
        None => "Ask it something or give it an order. Each message runs as a task.",
    };
    let lines = ChatLine::log(&view.chat, &view.feed.events);
    if lines.is_empty() || running.is_some() {
        list = list.push(muted(hint, tokens));
    }
    for line in lines {
        let bubble = mouse_area(
            container(text(cut(&line.text, 400)).size(15.0))
                .padding(10)
                .max_width(440)
                .style(styles::bubble(tokens, line.mine)),
        )
        .on_press(Message::Go(Screen::Task(line.task)))
        .interaction(mouse::Interaction::Pointer);
        list = list.push(if line.mine {
            row![Space::new().width(Length::Fill), bubble]
        } else {
            row![bubble, Space::new().width(Length::Fill)]
        });
    }
    let mut body = column![list].spacing(12);
    if !view.bot.status.accepts_tasks() {
        body = body.push(muted("Turn it on to talk to it.", tokens));
    }
    body.into()
}

/// A text cut to `max` characters with an ellipsis.
fn cut(words: &str, max: usize) -> String {
    if words.chars().count() <= max {
        return words.to_string();
    }
    format!("{}…", words.chars().take(max.saturating_sub(1)).collect::<String>())
}

/// What the bot is doing now and, once asked for, the urgent order that
/// stops it.
fn now_and_order<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    if !app.order.is_open_for(&bot.id) {
        return now(app, bot, tokens);
    }
    column![now(app, bot, tokens), super::super::order::form(app, bot, tokens)]
        .spacing(14)
        .into()
}

/// What the bot is doing right now, in a sentence, and its newest step.
fn now<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let tasks = app.fleet.as_ref().map(|f| f.tasks.as_slice()).unwrap_or_default();
    let events = app.feed().map(|f| f.events.as_slice()).unwrap_or_default();
    let state = BotNow::of(bot, tasks, events);
    let (headline, detail) = match &state {
        BotNow::Waiting { task } => (task.headline(70), "It waits for your OK before it goes on.".to_string()),
        BotNow::Working { task, since, last } => {
            let mut detail = format!("for {}", format::span(app.now - *since));
            if let Some(step) = last {
                let what = match step.detail() {
                    words if words.is_empty() => step.label(),
                    words => format!("{} · {words}", step.label()),
                };
                detail = format!(
                    "{detail} · last step: {}, {}",
                    cut(&what, 60),
                    format::ago(step.created_at, app.now)
                );
            }
            (task.headline(70), detail)
        }
        BotNow::Queued { task } => (task.headline(70), "Queued: it waits for a free node.".to_string()),
        BotNow::Off(BotStatus::Paused) => (
            "Paused".to_string(),
            "It takes no new task until you turn it on.".to_string(),
        ),
        BotNow::Off(_) => (
            "Not switched on".to_string(),
            "Turn it on to give it tasks.".to_string(),
        ),
        BotNow::Idle => ("Idle".to_string(), "It waits for a task.".to_string()),
    };
    let mut body = row![
        pill(Tone::of_now(&state), state.word(), tokens),
        column![bold(headline).size(18.0), muted(detail, tokens)]
            .spacing(4)
            .width(Length::Fill),
    ]
    .spacing(12)
    .align_y(Alignment::Center);
    if let Some(task) = state.task() {
        body = body.push(super::super::widgets::open(
            "Open task",
            Message::Go(Screen::Task(task.id.clone())),
            tokens,
        ));
    }
    body.into()
}

/// Its teams, and the field that changes them.
fn team<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let editing = app.watch.editing.as_ref().filter(|(id, _)| id == &bot.id);
    let body: El<'a> = match editing {
        Some((_, words)) => row![
            container(super::super::widgets::field(
                "Team",
                "ops",
                words,
                {
                    let id = bot.id.clone();
                    move |w| Message::TeamEdit(id.clone(), w)
                },
                Some(Message::TeamSave),
                tokens
            ))
            .width(Length::Fill),
            action(
                Kind::Primary,
                Icon::Check,
                "Save",
                (!app.switching).then_some(Message::TeamSave),
                tokens
            ),
            action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::TeamCancel), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::End)
        .into(),
        // Change is laid out first; the words or the teams take what it
        // leaves, on more lines in a narrow window.
        None => {
            let names: El<'a> = if bot.groups.is_empty() {
                muted("In no team: it knows no other bot", tokens)
                    .width(Length::Fill)
                    .into()
            } else {
                let mut chips = row![].spacing(8);
                for name in &bot.groups {
                    chips = chips.push(
                        mouse_area(chip(name.clone(), tokens))
                            .on_press(Message::Go(Screen::Team(name.clone())))
                            .interaction(mouse::Interaction::Pointer),
                    );
                }
                chips.width(Length::Fill).wrap().into()
            };
            row![
                names,
                action(
                    Kind::Ghost,
                    Icon::Team,
                    "Change",
                    Some(Message::TeamEdit(bot.id.clone(), bot.groups.join(", "))),
                    tokens,
                )
            ]
            .spacing(8)
            .align_y(Alignment::Center)
            .into()
        }
    };
    part("Team", body, tokens)
}

/// The bot's asks to hear a topic, each a card a person answers in one
/// click: «almacen asks to listen to error.*: "para reintentar"».
fn asks<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> Vec<El<'a>> {
    bot.pending_asks()
        .map(|ask| {
            let words = format!("{} asks to listen to {}", bot.id, ask.topic);
            let why = if ask.why.is_empty() {
                String::new()
            } else {
                format!("\u{201c}{}\u{201d}", ask.why)
            };
            let line = row![
                column![bold(words), muted(why, tokens)].spacing(2).width(Length::Fill),
                action(
                    Kind::Good,
                    Icon::Check,
                    "Approve",
                    (!app.switching).then(|| Message::ApproveAsk(bot.id.clone(), ask.topic.clone())),
                    tokens,
                ),
                action(
                    Kind::Secondary,
                    Icon::Cross,
                    "Deny",
                    (!app.switching).then(|| Message::DenyAsk(bot.id.clone(), ask.topic.clone())),
                    tokens,
                ),
            ]
            .spacing(12)
            .align_y(Alignment::Center);
            container(line)
                .padding(18)
                .width(Length::Fill)
                .style(styles::tone_card(tokens, Tone::Wait))
                .into()
        })
        .collect()
}

/// What the bot listens to: one line per topic with its mode and whom
/// from, Remove on each, and the form to listen to one more.
fn listens<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let mut body = column![].spacing(8);
    let active: Vec<_> = bot.subscriptions.iter().filter(|s| !s.is_pending()).collect();
    if active.is_empty() {
        body = body.push(muted(
            "Hears what is addressed to it and the team's stop and danger",
            tokens,
        ));
    }
    for sub in active {
        let mut line = row![
            bold(sub.topic.clone()),
            chip(sub.mode().name(), tokens),
            muted(format!("from {}", sub.senders()), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        if sub.urgent {
            line = line.push(pill(Tone::Wait, "urgent", tokens));
        }
        line = line.push(Space::new().width(Length::Fill)).push(action(
            Kind::Ghost,
            Icon::Cross,
            "Remove",
            (!app.switching).then(|| Message::Unsubscribe(bot.id.clone(), sub.topic.clone())),
            tokens,
        ));
        body = body.push(line);
    }
    if app.listen.is_open_for(&bot.id) {
        let form = &app.listen;
        body = body.push(
            column![
                super::super::widgets::field(
                    "Topic",
                    "pedido.* or warn.stock",
                    &form.topic,
                    Message::ListenTopic,
                    Some(Message::ListenSave),
                    tokens
                ),
                row![
                    column![
                        bold("Mode").size(SMALL),
                        pick_list(ModeChoice::ALL, Some(form.mode), Message::ListenMode)
                            .padding(8)
                            .style(styles::picker(tokens))
                            .menu_style(styles::menu(tokens)),
                        muted(form.default_label(), tokens),
                    ]
                    .spacing(6),
                    container(super::super::widgets::field(
                        "From",
                        "anyone",
                        &form.from,
                        Message::ListenFrom,
                        Some(Message::ListenSave),
                        tokens
                    ))
                    .width(Length::Fill),
                ]
                .spacing(10),
                row![
                    action(
                        Kind::Primary,
                        Icon::Check,
                        "Listen",
                        (!form.busy && !form.topic.trim().is_empty()).then_some(Message::ListenSave),
                        tokens
                    ),
                    action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::ListenClose), tokens),
                ]
                .spacing(10),
            ]
            .spacing(10),
        );
    }
    let open = action(
        Kind::Ghost,
        Icon::Plus,
        "Listen to a topic",
        (!app.listen.is_open_for(&bot.id)).then(|| Message::ListenOpen(bot.id.clone())),
        tokens,
    );
    part("Listens to", column![open, body].spacing(8), tokens)
}

/// The calls of this bot waiting for a person, each answered in one
/// click.
fn asking<'a>(app: &'a App, bot: &BotId, tokens: Tokens) -> Option<El<'a>> {
    let fleet = app.fleet.as_ref()?;
    let waiting: Vec<_> = fleet.waiting().into_iter().filter(|t| &t.bot_id == bot).collect();
    if waiting.is_empty() {
        return None;
    }
    let mut list = column![section_title(Icon::Alert, "It asks before acting", tokens)].spacing(12);
    for task in waiting {
        let wants = fleet
            .call_of(&task.id)
            .map(|call| format!("wants {}", call.summary(48)))
            .unwrap_or_default();
        list = list.push(
            row![
                column![bold(task.headline(60)), muted(wants, tokens)]
                    .spacing(2)
                    .width(Length::Fill),
                super::super::decide::buttons(app, &task.id, tokens),
            ]
            .spacing(12)
            .align_y(Alignment::Center),
        );
    }
    Some(
        container(list)
            .padding(18)
            .width(Length::Fill)
            .style(styles::tone_card(tokens, Tone::Wait))
            .into(),
    )
}

/// What a person rarely changes: how much the bot may do alone, then its
/// triggers, placement, tools, model and budgets.
fn advanced<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let mut choices = column![].spacing(10);
    let mut offered = levels::CHOICES.to_vec();
    if !offered.contains(&bot.level) {
        offered.push(bot.level);
    }
    for level in offered {
        let current = level == bot.level;
        let pick = (!current && !app.switching).then(|| Message::SetLevel(bot.id.clone(), level));
        choices = choices.push(levels::choice(level, current, pick, tokens));
    }
    choices = choices.push(muted("A change counts from its next step.", tokens));

    let mut triggers = column![].spacing(8);
    for trigger in &bot.triggers {
        triggers = triggers.push(
            row![
                chip(trigger.trigger_type.label(), tokens),
                bold(trigger.display_name().to_string()),
                muted(trigger.schedule(), tokens)
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        );
    }
    let facts = column![
        fact("Starts by", triggers, tokens),
        fact(
            "Runs on",
            reader(
                &app.readers,
                ReaderKey::BotRunsOn,
                bot.placement.describe(),
                Voice::Body,
                tokens
            ),
            tokens
        ),
        fact("Tools", tools(bot, tokens), tokens),
        fact(
            "Model",
            reader(
                &app.readers,
                ReaderKey::BotModel,
                passages::model(bot),
                Voice::Body,
                tokens
            ),
            tokens
        ),
        fact(
            "Budgets",
            reader(
                &app.readers,
                ReaderKey::BotBudgets,
                passages::budgets(bot),
                Voice::Body,
                tokens
            ),
            tokens
        ),
    ]
    .spacing(12);
    column![part("How much may it do alone", choices, tokens), facts]
        .spacing(16)
        .into()
}

fn tools<'a>(bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let mut chips = row![].spacing(6);
    for tool in &bot.allowed_tools {
        chips = chips.push(chip(tool.clone(), tokens));
    }
    let mut body = column![chips.wrap()].spacing(10);
    for rule in &bot.tool_rules.allow {
        body = body.push(row![icon(Icon::Check, 14.0, tokens.good), text(rule.describe()).size(SMALL)].spacing(8));
    }
    for rule in &bot.tool_rules.deny {
        body = body.push(row![icon(Icon::Cross, 14.0, tokens.bad), text(rule.describe()).size(SMALL)].spacing(8));
    }
    body.into()
}

/// A copy of the events, newest first as they show, once there is one.
fn event_tools<'a>(app: &'a App, tokens: Tokens) -> Vec<El<'a>> {
    match app.feed() {
        Some(feed) if !feed.events.is_empty() => vec![copy_button(
            "Copy events",
            format::events(feed.events.iter().rev()),
            tokens,
        )],
        _ => Vec::new(),
    }
}

/// The bot's events, newest first: the time, what happened and its
/// task's id on a line, the detail under it. The id is short and laid out
/// before the words, which wrap in what is left: the column is narrow, a
/// line of all four would push the id out of the card.
fn events<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(feed) = app.feed() else {
        return empty(Icon::Clock, "No event yet", tokens);
    };
    let line_height = LineHeight::default().to_absolute(SMALL.into());
    let mut list = column![].spacing(8);
    for event in feed.events.iter().rev().take(15) {
        let tone = Tone::of_event(event.tone());
        let task = TaskId::new(&event.task_id).ok();
        let mut head = row![bold(event.label()).size(SMALL).width(Length::Fill)].spacing(10);
        if let Some(task) = &task {
            head = head.push(muted(format::short_id(task.as_str(), 9), tokens).wrapping(Wrapping::None));
        }
        let mut words = column![head].spacing(2).width(Length::Fill);
        let detail = format::event_detail(event);
        if !detail.is_empty() {
            words = words.push(muted(detail, tokens));
        }
        let line = row![
            muted(format::clock(event.created_at), tokens).wrapping(Wrapping::None),
            container(icon(tone.event_icon(), 14.0, tone.color(&tokens))).center_y(line_height),
            words,
        ]
        .spacing(10);
        list = match task {
            Some(task) => list.push(
                mouse_area(line)
                    .on_press(Message::Go(Screen::Task(task)))
                    .interaction(mouse::Interaction::Pointer),
            ),
            None => list.push(line),
        };
    }
    if feed.events.is_empty() {
        return empty(Icon::Clock, "No event yet", tokens);
    }
    list.into()
}
